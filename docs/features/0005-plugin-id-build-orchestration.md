---
status: exploring
date: 2026-05-03
promotion-criteria: |
  Promote to `proposed` once a path is selected. Selection
  requires:

  1. A user need that's blocked by the absence of content-
     addressed plugin ids — today none exists; the v0
     package-name convention from FDR 0004 is sufficient until
     out-of-tree plugins or cross-binary plugin identity
     become load-bearing.
  2. Agreement on whether the chosen mechanism affects the
     blob-store wire format (it shouldn't — the right side of
     `<type-tag>@<id>` is opaque to the wire format) or only
     the runtime resolution / build orchestration (where the
     work actually lives).
  3. A demonstrated build pipeline that produces stable ids
     across two clean rebuilds of the same source on different
     machines.
---

# Plugin id build orchestration

## Problem Statement

FDR 0004 introduces the `<type-tag>@<builtin-plugin-id>`
reference format for plugins. The right side is intended to be
**content-addressed**: two binaries built from identical plugin
source produce identical ids; binaries with divergent code
produce different ones. This is the foundation for "horizontal
versioning" — the plugin's identity is its content, not a
hand-managed version number.

V0 ships with a stop-gap convention: `<builtin-plugin-id>` is
the leaf name of the Go package housing the plugin's factory
(e.g. `madder-codec-zstd-v1@zstd`). This is fine for shipping
the plugin architecture, but it isn't actually content-addressed —
two binaries with subtly different `zstd` packages would still
both report `@zstd`, masking real divergence.

This FDR captures the design space for replacing the package-
name convention with a real content-addressed mechanism. It does
**not** propose a chosen mechanism; the goal is to fix the
options in writing so a future implementer (or a future demand
for cross-binary plugin identity) has the trade-offs visible.

## Considered Paths

### Path A — Per-plugin nix derivation

Each plugin lives in its own (small) flake output: a derivation
that builds the plugin's package alone. Nix's content-addressed
store hashes the derivation's inputs and emits a deterministic
store path. The store path's hash IS the builtin-plugin-id.
At link time, the main madder binary embeds each plugin's
derivation hash via `-X main.builtinPluginID_<name>` ldflags.

**Engineering shape:** ~100 LoC per plugin in flake outputs;
~50 LoC in default.nix to wire the per-plugin derivations and
collect their hashes for ldflag injection; flake outputs grow
with every new plugin.

**Strengths.** Highest fidelity. Two binaries built from
identical plugin source on different machines under the same nix
toolchain produce bit-for-bit identical store paths and thus
identical ids. The mechanism is symmetric with how nix already
identifies madder itself. Out-of-tree plugins fit naturally
later: a third party publishes their plugin as a flake output;
its store-path hash IS its id; `madder` resolves
`<type-tag>@<store-path-hash>` via a per-store registry.

**Weaknesses.** Heaviest build orchestration. Every plugin
becomes a derivation with its own pwd, modules, build hooks.
Plugins that share a Go module (which all in-tree plugins do)
have to be carefully separated to keep their derivations from
trampling each other. Eval cost scales with plugin count — fine
at 4, possibly slow at 40. Non-nix-built madder builds (`go
build` directly) lose the ids unless a fallback hash mechanism
is wired in.

### Path B — Per-package source-tree hash

At build time, a small tool walks each plugin's package and its
transitive dependencies, hashes their `.go` source files in a
canonical order, and produces a digest. The main binary embeds
each digest via `-X` ldflags as in Path A.

**Engineering shape:** ~150 LoC for the hash tool (canonical
file ordering, transitive-dep walk, hashing); ~30 LoC in the
build (justfile + default.nix) to invoke it and forward the
results to ldflags. No per-plugin derivations.

**Strengths.** Works under both nix and bare `go build`.
Doesn't require splitting the Go module. Fast — a few-MB hash
per plugin. Forward-compatible with Path A: nix builds can
later compute Path A's id and check it matches Path B's hash
for self-validation.

**Weaknesses.** Hash semantics are subtler than they look.
Transitive deps include the standard library, which means a Go
toolchain bump can rev every plugin's id even if no plugin code
changed. Solutions: hash only the plugin's own package + direct
imports inside the madder module, ignoring stdlib; document
that toolchain identity is implicit in madder's overall
versioning. Either way, the identity model is "plugin source +
direct deps" rather than "plugin's compiled behavior" — a
reordering of unused imports could rev the id, while a `//
go:build` flag flip that genuinely changes behavior might not
if it doesn't change source bytes.

### Path C — Reproducible-build object hash

Compile each plugin's package separately under `-trimpath` and
deterministic flags, hash the resulting `.a` archive, embed the
hash via ldflags. The id is the hash of the actual compiled
plugin code, not its source.

**Engineering shape:** ~200 LoC. Requires per-package separate
compilations (which Go's build tooling supports via `go build
-o <pkg>.a`) plus careful flag normalization to make builds
reproducible across machines.

**Strengths.** Bridges A and B: ids change when behavior
changes (compiled code differs) but not just when source-byte-
level details rev (formatting, comments, dead imports —
compiler eliminates them). Doesn't require per-plugin nix
derivations; works under both nix and `go build`. Reproducible-
build flags are well-trodden territory in the Go ecosystem.

**Weaknesses.** Reproducibility is fragile in practice — Go
compiler patches across point releases can produce different
object code even for "the same" source. Path C is content-
addressed by *behavior under a specific compiler*, not by
source. That's potentially what we want (id changes mean
behavior changes) but it makes id stability dependent on the
toolchain. Cross-toolchain id portability is harder than under
A or B.

## Comparison

| Dimension | A (nix) | B (source hash) | C (object hash) |
|-----------|---------|-----------------|-----------------|
| Fidelity to actual code | high | medium (source ≠ behavior) | high (behavior ≠ source) |
| Cross-toolchain stability | high (nix pins) | medium (stdlib hash) | low (compiler-dep) |
| Works under bare `go build` | no | yes | yes |
| Build complexity | heavy | light | medium |
| Out-of-tree plugin fit | natural | viable | viable |
| Identity changes when…   | source or input changes | source bytes change | object bytes change |
| Toolchain bump revs ids | no (nix-pinned go) | yes if stdlib hashed | yes |

## Layering and Compatibility

The chosen mechanism is **invisible to FDR 0004's wire format
and runtime API**. The right side of `<type-tag>@<id>` is
opaque to consumers; only the in-binary registry's keys
change. Migration from the v0 package-name convention to any of
A/B/C is a build-orchestration change plus a documentation note
("the registry now keys on `<digest>` instead of `<package-leaf-
name>`"). Existing on-disk data (none, because v0 has no shipped
plugin-using configs yet) is unaffected.

The selected mechanism does shape the **runtime resolution
story for future out-of-tree plugins**:

- Under A, a remote plugin's id is a nix store path hash; the
  resolution registry maps that to a fetched store path.
- Under B, the id is a source-tree digest; resolution maps to a
  fetched source bundle that gets compiled in-process or by a
  helper.
- Under C, the id is an object hash; resolution maps to a
  fetched precompiled object that gets dlopened or pipelined.

Out-of-tree plugins are FDR 0004's "decide later" item. The
choice here gates how that's implemented when the time comes.

## Gating Questions

Before promoting this FDR to `proposed`, answer:

1. **Is there a concrete user demand for content-addressed
   plugin ids?** Today's v0 package-name convention is enough
   for the in-tree case. Demand for content-addressing comes
   from cross-binary identity (e.g. "store written by binary X
   should be readable by binary Y if and only if they share
   plugin Z") and out-of-tree plugins. If neither is on the
   roadmap, this FDR can stay `exploring` indefinitely.
2. **Is the toolchain stability model "nix-pinned go" or "any
   go that compiles"?** Path A assumes the former; Paths B and
   C are friendlier to the latter. Madder's CI today builds
   under both nix and bare `go build`; the answer affects which
   path is even viable.
3. **Will out-of-tree plugins ship?** If yes, the resolution
   model from §Layering becomes load-bearing and the choice is
   forced. If no, this is a build hygiene question and any of
   the three is acceptable.
4. **What's the cost of revving an id?** Today, no on-disk data
   pins ids (v0 is unshipped). Once V4 stores are written with
   plugin references, an id rev means existing stores need
   their config updated to reference the new id. Path A's
   stable-under-nix property minimizes spurious revs; Paths B
   and C rev more readily.

## Limitations

This FDR is in `exploring` status. It does **not** propose an
implementation, build pipeline, or interface — only the design
space and the candidate paths. Concrete tooling (the source-
hash binary, the per-plugin flake outputs, the deterministic-
compile flags) is deferred until a path is selected.

## More Information

- `docs/features/0004-blob-encoding-plugins.md` — the parent
  architecture this FDR refines. FDR 0004 ships the v0 package-
  name convention as a stop-gap; this FDR plans the content-
  addressed replacement.
- `docs/features/0010-zstd-dict-hints.md` (forthcoming) — adds
  the `zstd-with-dict` plugin on top of FDR 0004's architecture.
  Independent of this FDR; ships under v0 conventions.
- Go reproducible builds: <https://go.dev/blog/forward-compatible-go-toolchain>
  and `-trimpath` documentation describe the toolchain
  guarantees Path C relies on.
- Nix content-addressed derivations: <https://nixos.org/manual/nix/stable/store/store-object/content-address.html>
  describes the hashing scheme Path A leans on.
