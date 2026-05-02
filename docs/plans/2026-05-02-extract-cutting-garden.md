# Plan: Extract `tree-capture` / `tree-restore` into `cutting-garden`

## Context

`tree-capture` and `tree-restore` currently live in madder's CLI as
subcommands of the `madder` utility (`go/internal/india/commands/
tree_capture.go`, `tree_restore.go`). They share infrastructure with
the rest of madder — blob stores, markl IDs, hyphence, the futility
command framework — but operationally they're a different shape: a
filesystem-tree producer/consumer pair on top of a blob store, rather
than blob-CRUD primitives.

This plan extracts them into a new binary, **`cutting-garden`**
(aliased `cg`), that ships from this repo for now and is intended to
move to its own repo once it stabilizes. It mirrors the pattern
established by `madder-cache` — a sibling utility built from the same
go module, with its own `Utility` and its own command package.

### Cuscuta-universe positioning

Cutting-garden is part of the **cuscuta** universe of tools. Its
position in the dependency stack is:

```
                  ┌─ nebulous ─┐
   madder  ◀── cutting-garden ◀┼─ chrest    ─┤
                  └─ dodder   ─┘
```

- **Downstream of madder**: cutting-garden writes its captured
  blobs and receipts into madder blob stores. It depends on
  madder as its storage substrate — no parallel store
  implementation, no fork.
- **Upstream of nebulous, chrest, and dodder**: those projects
  consume cutting-garden's capture/restore primitives (as a Go
  library *and* as a CLI; the binary is the public surface, the
  Go packages are co-equal).

The longer-term shape of cutting-garden is a **plugin host for
capture/restore lifecycles across heterogeneous media**:
filesystems today, then orgmode, caldav, GitHub issues, git
repos, and more. Each medium becomes a plugin that owns its
capture walk, its receipt schema, and its restore materialization.
The current `tree-capture` / `tree-restore` are best understood
as the FS plugin — they will eventually be siblings of
`orgmode-capture`, `git-capture`, etc.

### Scope of this plan

This plan deliberately stays **narrow**: extract the existing two
commands into a new binary, no plugin abstraction. The plugin
seam is a follow-up plan — designed once we have the second real
plugin in hand, so we don't pick the wrong abstraction with N=1.

Specifically out of scope here:

- **Plugin abstraction / registry**. Today `tree-capture` and
  `tree-restore` are direct `utility.AddCmd(...)` registrations
  on the cutting-garden utility, no different from how madder
  registers its own commands. When the plugin abstraction lands
  it'll be **both** in-process and out-of-process: native Go
  plugins register in-process at `init()` time (mirroring
  `utility.AddCmd`); non-Go plugins talk over a subprocess
  boundary modeled on `hashicorp/go-plugin` (RPC over stdio,
  versioned handshake, the host owns lifecycle). In-process
  Go ships first because cuscuta is a Go ecosystem and the
  type-safety pays for itself with N=2; the subprocess seam is
  added when the first non-Go plugin actually shows up. Either
  way, that's a follow-up plan, not this one.
- **Generalizing the receipt format**. The hyphence type
  `madder-tree_capture-receipt-v1` stays as today's wire format.
  How tags get namespaced when there are multiple plugins
  (per-plugin tags? envelope + inner blob? plugin-id + opaque
  payload?) is deferred to the same generification pass. With
  N=1 every choice is a guess; we'll pick once we have a real
  second plugin to fit against.
- **The eventual repo split**. Cutting-garden moves out of this
  repo at some point; this plan only sets up the in-repo binary
  and tees up the move.

## Non-goals

- Renaming the wire-format type tag `madder-tree_capture-receipt-v1`.
  Per the project's "Legacy wire format — intentional, do not rename"
  bucket (CLAUDE.md), this stays as-is. Once `cutting-garden` has its
  own repo, it joins the same bucket alongside the dodder constants.
- Carving the receipt format library (`charlie/tree_capture_receipt/`)
  or sink library (`charlie/tree_capture_sink/`) into a new package
  namespace. They stay where they are; the eventual repo move handles
  that. Holding off keeps this change small and reviewable.
- Splitting `arg_resolver`. It's used by `cat`, `has`, `write`,
  `pack_blobs`, `commands_cache/cat`, `commands_cache/write` *and*
  tree-capture — it stays shared infrastructure in
  `go/internal/charlie/arg_resolver/`. Cutting-garden will continue to
  import it from madder's tree until the repo split, at which point
  it gets vendored into dewey or the new repo.
- MCP exposure. Tree-capture/restore aren't currently exposed via
  madder's MCP server (no references in `go/internal/hotel/`); they
  won't be in cutting-garden either, at least not in this plan.

## New repo layout (additions)

```
madder/
  go/
    cmd/
      cutting-garden/main.go                # new entry point
    internal/
      india/
        commands_cutting_garden/            # new commands package
          main.go                           # utility = NewUtility("cutting-garden", ...)
          tree_capture.go                   # moved from commands/
          tree_restore.go                   # moved from commands/
```

Unchanged but cross-edited:

```
go/cmd/madder-gen_man/main.go               # append commands_cutting_garden.GetUtility()
go/default.nix                              # add subPackage; postInstall symlink cg → cutting-garden
go/internal/india/commands/tree_capture.go  # DELETED
go/internal/india/commands/tree_restore.go  # DELETED
docs/man.7/tree-capture-receipt.md          # prose: madder → cutting-garden
docs/rfcs/0003-tree-capture-restore-rules.md# prose: madder tree-capture/restore → cutting-garden
zz-tests_bats/tree_capture.bats             # run_cg / require_bin CG_BIN
zz-tests_bats/tree_restore.bats             # run_cg / require_bin CG_BIN
```

## Steps

### Phase 1: New utility skeleton

1. **Create `go/internal/india/commands_cutting_garden/main.go`** —
   declare `var utility = futility.NewUtility("cutting-garden",
   "filesystem-tree capture and restore over content-addressable
   blob stores")`. Mirror `commands/main.go`'s shape: examples, files,
   env vars, global flags. The `--no-inventory-log` global flag and
   inventory-log file paths still apply (writes go through madder's
   blob store, which still emits the inventory log) — copy that
   wiring verbatim. Export `GetUtility()`.

2. **Create `go/cmd/cutting-garden/main.go`** — copy
   `cmd/madder-cache/main.go` and swap the import + `cli_main.Run`
   name to `"cutting-garden"`. Same `version`/`commit` ldflag stubs
   and `buildinfo.Set` + `markl_registrations` blank-import.

### Phase 2: Move the commands

3. **Move `tree_capture.go` and `tree_restore.go`** from
   `commands/` to `commands_cutting_garden/`. Update the package
   declaration. The `init()` calls (`utility.AddCmd(...)`) now bind
   to the cutting-garden `utility` defined in step 1 — no other
   change needed.

4. **Audit imports** — confirm no symbol leaked into `commands/`
   that's now only reachable from `commands_cutting_garden/`. The
   private helpers in `tree_capture.go` (`planCapture`, `walkRoot`,
   `classifyArg`, `checkRootScope`, `checkRootCollisions`, etc.) and
   in `tree_restore.go` move with their files. `computeStoreHint`
   is referenced from both files — both move together, so the shared
   helper goes with them; pick one file to host it.

5. **Delete** `go/internal/india/commands/tree_capture.go` and
   `tree_restore.go`. Run `go build ./...` to confirm `commands/`
   doesn't reference them transitively (it shouldn't — they're
   self-contained subcommand files).

### Phase 3: Build wiring

6. **`go/cmd/madder-gen_man/main.go`** — append
   `commands_cutting_garden.GetUtility()` to the `utilities` slice so
   the build emits `cutting-garden(1)`, `cutting-garden-tree-capture(1)`,
   and `cutting-garden-tree-restore(1)` man pages alongside madder's.

7. **`go/default.nix`** — add `"cmd/cutting-garden"` to the
   `subPackages` list inside `madder = pkgs.buildGoApplication {...}`.
   In `postInstall`, after the `madder-gen_man` invocation, add
   `ln -s cutting-garden $out/bin/cg` so the alias ships with the
   package.

   **Trade-off note**: a symlink means `cg --help` prints
   "cutting-garden ..." in the usage banner (because
   `cli_main.Run(utility, "cutting-garden")` hardcodes the name).
   That's fine — `cg` is documented as an alias, not a separate tool.
   The alternative — a second `cmd/cg/main.go` calling
   `cli_main.Run(utility, "cg")` — doubles the binary count and
   forks the help banner. Skip it.

   **No separate flake output**: cutting-garden ships as a binary
   inside the existing `madder` derivation (`subPackages` entry).
   Consumers that want just cutting-garden read
   `${madder}/bin/cutting-garden` from the same store path — no
   `packages.cutting-garden` flake output today. A separate output
   becomes interesting at the eventual repo split, when madder and
   cutting-garden are separately versioned; introducing it pre-split
   forks the release cadence for no functional gain.

### Phase 4: Tests

8. **`zz-tests_bats/lib/common.bash`** — add a `require_bin CG_BIN
   cutting-garden` helper alongside the existing `MADDER_BIN`
   pattern, plus a `run_cg` wrapper that mirrors `run_madder`.

9. **Migrate `tree_capture.bats` and `tree_restore.bats`** —
   replace every `run_madder tree-capture` / `run_madder tree-restore`
   with `run_cg tree-capture` / `run_cg tree-restore`. Setup that
   creates a blob store still uses `run_madder init` — these tests
   exercise the producer/consumer pair, not the store-management
   primitives. Receipt-blob retrieval (`madder cat <id>`) likewise
   stays on `run_madder`. There are no shared fixture files — both
   `.bats` files build their input trees inline in `setup()` — so the
   migration is a `run_madder` → `run_cg` find-and-replace per file.

10. **`go/default.nix`** — extend `mkBatsRunCommand` to export
    `CG_BIN` alongside `MADDER_BIN`. The default
    `madderBin = "$out/bin/madder"` parameter pattern grows a sibling
    `cgBin = "$out/bin/cutting-garden"` (default), threaded through
    `mkBatsLane` callers. Existing per-tag lanes (`bats-tree_capture`,
    `bats-tree_restore`) auto-discover from file_tags so they don't
    need new entries — they just need `CG_BIN` to exist.

    **Parameterization deferred**: `mkBatsRunCommand` becomes the
    second binary-pointer this helper threads. The principled fix
    — generalizing to a `binaries` map (env-var-name → store path) —
    is the upstream move tracked by amarbel-llc/nixpkgs#14. Resist
    doing it inline here: ad-hoc `cgBin` keeps the cutting-garden
    PR small, and the upstream issue gets a real second consumer to
    validate its shape against.

11. **`commands/main_test.go`** is a no-op for this plan. The single
    test (`TestUtilityHasCommands`) only asserts that some commands
    are registered (`count > 0`); it doesn't enumerate names. Removing
    `tree-capture`/`tree-restore` won't break it. Verify
    post-extraction.

### Phase 5: Documentation

12. **`docs/man.7/tree-capture-receipt.md`** — rewrite prose
    references from "madder tree-capture" to "cutting-garden
    tree-capture" (and tree-restore). Keep the wire-format type tag
    `madder-tree_capture-receipt-v1` verbatim — it's the on-disk
    identifier that consumers pattern-match.

13. **`docs/rfcs/0003-tree-capture-restore-rules.md`** — same
    prose update. The RFC already speaks of "any conformant
    producer/consumer," so the body is largely already abstract;
    only the worked examples and the producer/consumer role
    definitions need touching.

14. **`go/internal/india/commands_cutting_garden/CLAUDE.md`** —
    short package note pointing at RFC 0003 and the receipt man7
    page. (Mirror the brevity of `commands/CLAUDE.md`.)

### Phase 6: Repo-level housekeeping

15. **Update repo-root `CLAUDE.md`** — add a paragraph under "How
    this repo is organized" (or equivalent) describing the
    `cutting-garden` binary and its eventual move to its own repo.
    Add a note that the wire-format tag stays `madder-...`, queued
    for the same treatment as the dodder constants when the repo
    split happens.

16. **`justfile`** — verify recipes don't hard-code only `madder` /
    `madder-cache` (e.g. install steps, smoke tests). If they do,
    extend to `cutting-garden`.

## Verification

- `nix build` succeeds and emits `bin/madder`, `bin/madder-cache`,
  `bin/cutting-garden`, and a `bin/cg` symlink to `cutting-garden`.
- `cg --help` works (banner says "cutting-garden", not "cg" — by
  design).
- `man cutting-garden`, `man cutting-garden-tree-capture`,
  `man cutting-garden-tree-restore` all render.
- `man 7 tree-capture-receipt` still resolves; prose says
  "cutting-garden tree-capture".
- `go test ./...` passes.
- `just test-bats` — `tree_capture.bats` and `tree_restore.bats`
  pass against `cutting-garden`; all other lanes still pass against
  `madder`.
- A receipt produced by the **old** `madder tree-capture` (e.g.
  written to a fixture before this change) is still consumable by
  **new** `cutting-garden tree-restore`. This is the wire-format
  compatibility check; it's why we don't rename the type tag.
- `madder tree-capture --help` no longer lists `tree-capture` as a
  subcommand (and the same for `tree-restore`).

## Decisions

The architectural questions are settled; the mechanical answers
below are the ones this plan will execute against.

**Architectural (locked):**

- *Scope*: narrow extraction only. No plugin seam, no receipt
  generification — those are follow-up plans, designed when there's
  a second real plugin to anchor against.
- *Plugin model (future)*: both. In-process Go registry first
  (init-time, mirroring `utility.AddCmd`). Non-Go plugins later via
  a `hashicorp/go-plugin`-inspired subprocess boundary. Decision
  recorded here so the eventual seam doesn't relitigate it.
- *Wire format (future)*: keep `madder-tree_capture-receipt-v1`
  unchanged today. How tags get namespaced across plugins is
  deferred to the generification pass.

**Mechanical (locked):**

1. *Remove from madder, not dual-host.* Dual-hosting drifts and
   defeats the extraction. The two source files move; madder loses
   the subcommands.
2. *`cg` alias = nix `postInstall` symlink.* `cg --help` prints
   "cutting-garden ..." in its banner; that's intentional, since
   `cg` is a documented alias, not a separate tool.
3. *Wire-format tag stays* `madder-tree_capture-receipt-v1`.
   Renaming orphans every existing receipt blob in user stores.
4. *Receipt and sink packages stay put* at
   `go/internal/charlie/tree_capture_receipt/` and
   `go/internal/charlie/tree_capture_sink/`. They migrate during
   the eventual repo split, not now.
5. *Inventory-log + `--no-inventory-log` global flag*: copy
   verbatim into the new utility. Cutting-garden writes through
   madder's blob store API and inherits the log behavior; the
   global flag needs to be plumbed so users can suppress.

## Risks

- **Test-suite churn**: 2 bats files migrate at once; if the helper
  pattern (`run_cg`) is wrong on the first try, both lanes go red
  together. Mitigate by adding the helper and converting
  `tree_capture.bats` first, getting it green, then doing
  `tree_restore.bats`.
- **Inventory-log audit drift**: madder's `--no-inventory-log` flag
  is wired in `commands/main.go`'s `GlobalFlagDefiner`. If the
  cutting-garden copy of that wiring drifts (different default,
  different env-var name), users get inconsistent suppression
  semantics across the two binaries. Mitigate with a shared helper
  factored out of `commands/main.go` if the duplication smells —
  but only if it smells; one verbatim copy is cheap. If it does need
  factoring, the helper lives in `golf/command_components/`
  (alongside `EnvBlobStore`), where structurally-typed cross-utility
  glue already congregates.
- **RFC / man7 prose drift**: `tree-capture-receipt(7)` and RFC
  0003 are user-facing specs; rewording "madder" to "cutting-garden"
  is mechanical but easy to miss in one spot. Final pass with
  `grep -rn 'madder tree-' docs/` after Phase 5.
- **Wire-format tag dissonance**: `cutting-garden` writes blobs
  whose hyphence type is `madder-tree_capture-receipt-v1`. Users
  will see this and ask why. Address in `cutting-garden(1)`'s
  long description: a brief "the wire format is shared with madder
  for compatibility; the tag will be revisited if cutting-garden
  ever forks the format."
