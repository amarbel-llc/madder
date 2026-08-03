---
status: proposed
date: 2026-08-03
promotion-criteria: |
  Promote to `experimental` once a single normative resolver
  (`scoped_id.Id` -> physical location) exists and is the sole path
  used by both the init and the operate call sites, the conformance
  suites (`go/internal/echo/env_dir/resolution_conformance_test.go` and
  `zz-tests_bats/resolution_conformance.bats`) pass with every
  EXPECTED-FAIL marker removed, and the divergent functions named in
  the Divergence Inventory are either deleted or reduced to thin
  callers of the one resolver, and the `grammar-vectors` gate is green
  with langlang wired as a hard dependency (flake-input-go_mod). Promote
  to `accepted` once at least one downstream (the dodder FDR-0019
  revision) resolves repo ids through the same single-resolver contract,
  the implicit-merge read paths (`makeAncestorOverrideStores`,
  `blobFromRemainingStores`) are removed, and the instance-identity layer
  has shipped — every blob store carrying a minted uuidv7, the
  copy-migration command, and the normative mismatch-diagnosis.
---

# Scoped-ID Resolution

## Problem Statement

A madder blob-store id — and, downstream, a dodder repo id — is a
**scoped id**: a name, an optional scope prefix, and (for the CWD
scope) a dot-depth. The grammar is:

    name        XDG user scope        `$XDG_DATA_HOME/madder/blob_stores/<name>`
    .name       CWD scope, depth 0    nearest ancestor `.madder/…` store named <name>
    ..name      CWD scope, depth 1    next-nearest such ancestor
    //name      XDG system scope      `<SystemRoot>/blob_stores/<name>`
    %name       XDG cache scope       `$XDG_CACHE_HOME/madder/blob_stores/<name>`

The grammar is sound and this FDR does not change it. `scoped_id.Id`
parses it correctly and unambiguously: every id carries its scope in
its prefix, so no two scopes can collide, and no id is ambiguous about
which scope it names.

**The defect is not the grammar. It is resolution.** There is today no
single normative answer to "what physical location does id *X* refer
to." At least four independent resolution engines coexist, each with
deliberately different semantics for the *same* id:

1. **Literal init walk-up** —
   `env_dir.MakeDefaultAndInitialize`'s `LocationTypeCwd` branch
   (`resolveCwdAncestorOrError`, madder#153) roots at the *literal*
   Nth parent of `$PWD`, with **no** store-existence check. It may be
   initialising a store that does not exist yet, so it counts raw
   parent directories.
2. **Store-aware operate walk-up** —
   `directory_layout.ResolveNthAncestorMatch` (dodder#281) returns the
   Nth *matching* ancestor, skipping ancestors that do **not** carry a
   `.madder/` store of that name.
3. **Walk-up-immune home resolver** —
   `env_dir.MakeWithHomeAndInitialize` pins to the user `$HOME`/XDG
   with no walk-up at all. Reached by the XDG-user branch, by the
   "auto" default branch of `MakeDefaultAndInitialize`, and by dodder's
   `init-workspace` parent resolution.
4. **Implicit union-merge discovery** —
   `directory_layout.FindAllCwdOverridePaths` collects *every* ancestor
   `.madder/`, and `blob_stores.MakeBlobStores` merges the CWD-ancestor
   set, the XDG-user set, and the XDG-system set into one keyed map over
   which default-selection and per-command read-fallback then operate.

Engines (1) and (2) disagree about what `..name` means: literal counts
every parent, store-aware counts only matching parents — the same
`..name` resolves to a different directory depending on whether you are
on the init path or the operate path. `internal/echo/env_dir/AGENTS.md`
and both walk-up functions' own comments document this
literal-vs-store-aware split as *deliberate*. It is the live madder
residual of the broader disagreement class.

That broader class — two resolvers, invoked from different code paths,
landing on different physical stores, with nothing detecting the
divergence — has produced a family of real bugs:

- **madder#227** *(closed, fixed)* — the madder instance. `init
  <unprefixed-id>` landed the store in an ancestor `.madder` (a
  walk-up path) while `write <unprefixed-id>` resolved the XDG-user
  scope (no walk-up), so an init→write round trip failed inside any
  directory with an ancestor `.madder`. Fixed **point-wise** by
  home-pinning unprefixed init so it matches write (current
  `MakeDefaultAndInitialize` routes the XDG-user/unknown/zero id to
  `MakeWithHomeAndInitialize`; pinned by
  `TestInitBlobStore_UnprefixedIdIgnoresAncestorOverride`). The bare-name
  symptom is gone; the *structural* plurality that produced it is not.
- **dodder#359** *(open)* — the same class, still live cross-repo, and
  the sharpest statement of the mechanism. A blob written via dodder's
  walk-up-sensitive "auto" path lands in an ancestor `.madder`;
  `init-workspace`'s walk-up-immune home resolver then cannot find it.
  This FDR's motivating incident.
- **dodder#283** — a nested, same-named repo cannot find its own
  genesis-type blob, because a resolver shadows the child's store with
  the ancestor's.
- **dodder#341** — a fresh env cannot discover a pointer store written
  earlier in the same process.
- **dodder#196** — a clone split-brains its lookups across nested
  `.madder/` directories.

Each engine is individually intentional. The problem is that they
*coexist* and *disagree*, and nothing reconciles them — so a fix like
madder#227 patches one symptom point without removing the class. This
FDR specifies one resolver, collapses the four engines onto it so the
class cannot recur anywhere, and hands dodder the substrate to close
dodder#359.

## Terminology and Scope

The key words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are used
per RFC 2119.

**Scopes.** madder recognises four resolution scopes, spelled by the
`scoped_id` prefix (`go/internal/0/xdg_location_type`):

| Scope        | Spelling         | Root                                   |
| ------------ | ---------------- | -------------------------------------- |
| XDG user     | `name` (or `~name`) | `$XDG_DATA_HOME/madder/`            |
| CWD          | `.name`, `..name`, … | walk-up from `$PWD` (defined below) |
| XDG system   | `//name`, `/name`   | `<SystemRoot>/` (e.g. `/var/lib/madder`) |
| XDG cache    | `%name`          | `$XDG_CACHE_HOME/madder/`              |

`/name` (single slash) is the **remote-first** spelling: dodder
consults its remotes first and falls back to the system-scoped `name`.
madder has no remote transport and **MUST** resolve both `/name` and
`//name` to the XDG-system scope. The remote-first marker is carried
through (`scoped_id.Id.IsRemoteFirst`) for dodder but is inert in
madder.

**In scope for this FDR:** the normative id→location function, the init
exception, the resolve-time error contract, the retirement of implicit
union-merge, the legacy-layout error behaviour, and — since identity is
inseparable from resolution here — the blob-store **instance identity**
(the uuidv7), its copy-migration, and the mismatch-diagnosis that
composes with the error contract; all as they govern **madder**
blob-store resolution.

**Out of scope** (owned by the parallel revision of dodder's FDR-0019):
repo auto-id discovery rules, the `repos/<name>/` nesting *policy*, and
the `/name` remote-first *resolution* algorithm. This FDR defines the
substrate those dodder rules compose over; it does not define them.

## The Normative Resolver

There **MUST** be exactly one resolution function, of shape:

    Resolve(id scoped_id.Id, cwd string, env ResolutionEnv) (Location, error)

where `Location` is a physical filesystem path (the blob-store
directory). Every code path that turns a scoped id into a physical
location — init and operate, CLI arg and internal call, first command
and third command in a process — **MUST** obtain that location from
this one function. The function is **pure** with respect to
`(id, cwd, on-disk state, env)`: given the same inputs it **MUST**
return the same `Location`. Two invocations for the same id, from
different call sites, in the same process and on the same host,
**MUST NOT** return different locations. This is the single invariant
whose absence is dodder#359.

### Per-scope rules

**XDG user (`name`).** Resolves to `$XDG_DATA_HOME/madder/blob_stores/<name>`
(nested under `repos/<name>/` when a repo name is active, per
madder#241). There is **no** walk-up. A user-scoped id **MUST NOT**
resolve to, or be shadowed by, any CWD-ancestor `.madder/` store. This
is the rule whose violation is the #359 write-lands-in-ancestor half.

**XDG system (`//name`, `/name`).** Resolves to
`<SystemRoot>/blob_stores/<name>`. There is **no** walk-up. When no
`SystemRoot` is configured, a system-scoped id **MUST** fail to resolve
with the error contract below — it **MUST NOT** silently fall back to
the user layout (the `GetXDGForSystemBlobStores` `ok=false` case).

**XDG cache (`%name`).** Resolves to
`$XDG_CACHE_HOME/madder/blob_stores/<name>`. No walk-up.

**CWD (`.name`, `..name`, …).** Resolves by the walk-up defined next.
This is the **only** scope that walks up, and the walk-up applies
**only** to `.`-prefixed ids.

### Walk-up definition (CWD scope)

Let `depth = dots − 1` (`.name` → 0, `..name` → 1, …). Enumerate the
ancestor chain starting at `$PWD` and proceeding to each parent, up to
but not crossing a `<SCOPE>_CEILING_DIRECTORIES` boundary or the
filesystem root. An ancestor **matches** iff it carries a `.madder/`
blob store named `<name>`. Resolution returns the `depth`-th matching
ancestor, counted **deepest-first** (nearest to `$PWD` is index 0).

This is the **store-aware, deepest-first, match-ranked** walk —
today's `ResolveNthAncestorMatch` semantics. It is chosen as the one
CWD resolver because:

- It is what "the parent's store" means to a user standing in `$PWD`:
  the nearest enclosing repo, then the next, skipping directories that
  merely happen to be on the path but host no such store.
- It lets a command run from any child directory beneath the addressed
  store root (operate-from-below), which the literal walk cannot.
- The literal walk exists only to serve *init* of a not-yet-existing
  store, and init no longer derives its location from dot-depth (see
  The Init Exception). Once init stops walking, the literal walk has no
  remaining caller.

The walk **MUST** error, not clamp, when fewer than `depth + 1`
matching ancestors exist (see Error Contract). This preserves
`scoped_id`'s strict posture and madder#153's overflow policy.

### Ambiguity rules

- **Cross-scope: impossible.** Scope is determined syntactically by the
  prefix. `widgets`, `.widgets`, and `//widgets` are three distinct
  ids naming three distinct scopes. The resolver **MUST NOT** guess a
  scope; there is nothing to guess.
- **Within CWD scope: ordered, not ambiguous.** `.name` is
  unambiguously the deepest match, `..name` the next, and so on. Two
  ancestors of the same name are addressed by distinct dot-depths, not
  merged. `depth` overflow is an *error*, not a silent nearest-match
  fallback.
- **Within a single fixed scope (user/system/cache): one path.** There
  is exactly one location per id; no ambiguity is representable.

Because ambiguity is structurally excluded, every resolution outcome is
either *one location* or *a fail-fast error*. There is no third,
"merged" outcome (see Implicit Union-Merge Is Retired).

## The Init Exception

Init is **not a second resolver.** It is the one resolver plus a single
added permission: init **MAY** find nothing at the resolved location
and **create** there, where operate **MUST** error on a miss.

Init's creation location is defined as follows:

- **XDG user / system / cache init.** Create at the id's single fixed
  location (the per-scope rule above). Unchanged from operate except
  that absence is permitted-and-created rather than an error.
- **CWD init.** Create at **`$PWD`**. Init **MUST NOT** derive a
  creation location from dot-depth. A caller who wants a store in a
  particular directory `cd`s there and inits `.name`.
- **Multi-dot at init.** An init of a CWD-scoped id with `depth > 0`
  (`..name`, `...name`, …) **MUST** be rejected with an actionable
  error (see Error Contract). Dot-depth is an *addressing-only* spelling
  — it selects an existing ancestor — and there is no existing ancestor
  to select at creation time. Silently normalising `..name` to `.name`
  at `$PWD` would surprise a user who typed `..` meaning "my parent's
  store."

Init **MUST NOT** walk up to reuse an ancestor store. `init .name` at
`$PWD` creates a store at `$PWD` even when a same-named `.name` store
exists in an ancestor; the two are distinct stores at distinct depths,
and the operate resolver's deepest-first rule then addresses the new
one as `.name` and the ancestor as `..name`. This is what makes a
nested same-named repo able to find its own store (the dodder#283
class): init creates *here*, operate resolves *here-first*, and the two
agree because they share the one walk-up definition.

(Whether dodder's `repos/<name>/` layout *should* permit such nesting
at all is dodder-side policy, out of scope here. This FDR only fixes
the madder substrate so that if nesting occurs, resolution is
coherent.)

## Error Contract

Every resolution miss or rejection **MUST** fail fast at resolve time —
never a silent fallback, never a merged view — and the error **MUST**
state four things:

1. **What id was asked** — the canonical id string.
2. **Which scope and paths were searched** — the scope name and the
   concrete directories consulted (the ancestor chain for CWD; the one
   path for a fixed scope).
3. **What was found** — nothing, a legacy layout, or fewer than
   `depth + 1` matches (with the matches that *do* exist).
4. **What the user should do** — the exact command or action that
   resolves the miss.

### Example error texts

**CWD depth overflow** (asked `...widgets`, only two matching
ancestors):

    error: cannot resolve blob-store id "...widgets"
      scope:   CWD (walk-up from /home/u/proj/pkg/sub)
      found:   2 ancestor store(s) named "widgets":
                 .widgets   -> /home/u/proj/pkg/.madder/…/blob_stores/widgets
                 ..widgets  -> /home/u/proj/.madder/…/blob_stores/widgets
      wanted:  the 3rd-nearest (dot-depth 2); only 2 exist
      action:  address one of the stores listed above, or `cd` to a
               directory with a 3rd enclosing "widgets" store

**User-scope miss** (asked `widgets`, no such store):

    error: cannot resolve blob-store id "widgets"
      scope:   XDG user
      searched: /home/u/.local/share/madder/blob_stores/widgets
      found:   nothing
      action:  create it with `madder init widgets`, or did you mean a
               CWD store `.widgets`?

**System-scope with no SystemRoot** (asked `//widgets`):

    error: cannot resolve blob-store id "//widgets"
      scope:   XDG system
      found:   no system root is configured for this environment
      action:  run under a system-scoped environment (e.g. a
               `madder serve --store //widgets` daemon), or use a user
               (`widgets`) or CWD (`.widgets`) store

**Multi-dot init rejection** (asked `madder init ..gadgets`):

    error: cannot init blob-store id "..gadgets"
      reason:  dot-depth is an addressing-only spelling; init always
               creates in the current directory and never derives a
               creation location from dot-depth
      action:  `cd` to the directory where the store should live and
               run `madder init .gadgets`

## Implicit Union-Merge Is Retired

One id **MUST** resolve to exactly one store. The resolver **MUST NOT**
produce a merged, multi-store, read-with-fallback view under any id.

Concretely, the following implicit behaviours are retired:

- The CWD-ancestor union in `blob_stores.makeAncestorOverrideStores`
  (building a live store for *every* ancestor and read-falling-back
  across them).
- The cross-scope merge in `blob_stores.MakeBlobStores` that folds the
  CWD, user, and system sets into one map that default-selection and
  per-command fallback then reach into.
- The per-command `blobFromRemainingStores` walk (`cat`/`has`/`list`/
  `fsck`), which serves a blob from a *different* store than the one the
  id resolved to.

Multi-store read/write behaviour **MUST** exist **only** via an
explicit `store_type = "multi"` blob-store config (FDR-0009), whose
members are digest-pinned blob-store ids (FDR-0008 Phase 2). A user who
wants "read from the local store, fall back to the ancestor archive"
authors a `multi` config that names both members explicitly; the multi
store then resolves — as a single id — to a single `Multi` blob store
that encapsulates the fallback set. Ancestor stores are **never**
implicitly visible through a non-multi id.

**Enumeration is not resolution.** `madder list` **MAY** still walk the
ancestor chain and the XDG scopes to *display* every store it can find,
tagging same-named CWD ancestors with ascending dot-depth (`.widgets`,
`..widgets`) so the user can see and address each. Listing is a
discovery affordance and is unaffected by this rule. What is retired is
using that enumerated set as a *resolution* or *read-fallback*
substrate: a single id resolves through `Resolve`, to one store, full
stop.

## Legacy Layouts Are Errors

The resolver **MUST NOT** carry compatibility branches for legacy or
unrecognised on-disk layouts. When resolution reaches a location that
holds a legacy layout (e.g. a `dodder-blob_store-config` file, the
pre-rename config name kept only for detection in
`directory_layout.util`), the resolver **MUST** fail with a specific,
actionable error that names the id, the offending path, and the
migration tool to run — following the same error-contract shape above.

Today the only legacy handling is `GetBlobStoreConfigPaths`, which
globs for `dodder-blob_store-config` and errors *"rename each to
blob_store-config"* at enumeration time. That diverges from this
contract in two ways: it fires at glob time rather than per-id at
resolve time, and it prescribes a **manual `rename`** rather than a
**named migration tool**. The former `madder migrate-legacy-configs`
command was removed (its search path drifted out of sync with the
startup check — madder#28, closed — and it no longer exists in the
tree); no `madder` command migrates a legacy config today.

This tooling gap is **already tracked by the open madder#175**
("blob_store-config legacy rename error: emit a copy-pasteable shell
command (and/or auto-rename via ErrorRetryable)"). Rather than folding
migration tooling into this spec or filing a duplicate, the resolver's
contract *composes with* madder#175: once #175 lands a copy-pasteable
command or an `ErrorRetryable` in-place rename, the resolver's
legacy error names that recovery. Until then the legacy error remains
the glob-time manual form, and this FDR's per-id resolve-time legacy
error is a promotion-blocking convergence item, not a shipped behaviour.

## Instance Identity: the uuid

The resolver above answers *where* a scoped id points. This section
answers *which instance* is there. A config digest alone is **not**
identity: two blob stores configured identically produce byte-identical
configs and therefore identical digests, so a digest cannot distinguish
"the store I pinned" from "a different store that happens to be
configured the same." That gap is the identity half of the dodder#359
class — a `name@digest` reference can resolve to the wrong physical store
and a digest match would not notice. (The same problem exists on the
dodder side, where one yubikey-backed pubkey can legitimately back
several repos.)

Every blob store therefore carries a **uuid** (uuidv7) instance
identity, distinct from its config digest.

### The uuid field

- Every blob store **MUST** carry a uuidv7 instance identity, rendered as
  a markl id (`uuidv7-<blech32 payload>` — the 16 uuidv7 bytes
  blech32-encoded), stored inside its `blob_store-config`.
- The uuid **MUST** be minted once, at store init, and is **immutable**
  thereafter.
- The uuid **MUST NOT** be lazy-minted. Minting a uuid into an existing
  config on first read would rewrite the config bytes and thereby
  invalidate every digest pin taken against the pre-mint config (see
  *Digest entropy*) — a destructive side effect disguised as a read.
  Existing stores acquire a uuid only through the explicit migration
  below.
- The `uuidv7` markl **format** is registered at **madder's own**
  registration site (`go/internal/charlie/markl_registrations`): the repo
  that *defines* a format owns its registration. This is a deliberate
  extension of current policy — that site registers only *purposes* and
  aliases today, and *formats* have lived upstream in piggy (madder#255,
  the piggy#183 ownership inversion). uuidv7 is madder's first
  self-defined format, so madder gains a format-registration call against
  piggy's registry mechanism. Naming the site is in scope for this FDR;
  wiring the registration is not (this FDR is docs + skeleton). Registering
  the format is necessary but **not sufficient**: per **madder#278**, a
  markl registration only takes effect where `markl_registrations` is
  actually *imported*, so an external in-process consumer resolving a
  `uuidv7-…` id would hit the same invisible runtime footgun
  (`unknown format id`) that #278 documents for the age/pivy formats,
  unless the uuidv7 registration rides whatever activation path #278
  settles on — folding registrations into the store-package imports, or a
  fail-fast guard. The implementer inherits that constraint. Because a
  `uuidv7-<payload>` value is an ordinary markl-id `format-data` (the
  format-id slot is generic), the scoped-id grammar admits it with **no
  change** — see `scoped_id.peg`'s `DigestSlot`.

### Migration: hard cut and copy (never in-place mint)

The uuid-bearing config version is a **hard cut**, not an
additive-compatible bump, and existing stores migrate by **copy**, never
in place. The migration command **MUST**:

1. Create a **new** store with the new config flavor, minting its uuid at
   creation.
2. Populate the new store with the old store's objects.
3. Leave the **old** store untouched; the user deletes it when satisfied.

Precedent: dodder's `migrate-repo-layout`
(`code.linenisgreat.com/dodder`, issue #363) — a pure copy in which the
source is never modified. Consequences, stated normatively:

- Existing digest pins stay **coherent against the old store**: the old
  config is byte-unchanged, so its digest and every pin to it still
  resolve.
- Migration **re-points** references at the new store as part of the
  copy.
- **Sequencing:** dodder repos adopt the same copy-migration pattern
  **after** madder proves it here.

This **replaces** an earlier in-place-mint framing (shaped like
`config-pin_digest`'s rewrite): an in-place mint changes the config bytes
and so silently invalidates existing pins — exactly what the "never
lazy-mint" rule forbids. Copy-migration is the only sanctioned path.

### Digest entropy: FDR-0008 pins become instance pins

Because the uuid lives **inside** the config, the FDR-0008 Phase-1 config
digest — computed over the config body bytes — now covers the uuid.
Configs that were byte-identical (same store type, same options) no
longer are: each carries a distinct uuid, hence a distinct digest.

Therefore FDR-0008 digest pinning becomes true **instance** pinning with
**zero wire-format change**. A `name@digest` that matched two stores
before now matches exactly one, because the digest carries the instance's
entropy. The pin currency is unchanged: still `name@digest`. (A v2 that
pins `digest + signature` via a piggy/PAPI identity is out of scope here,
named so the slot is anticipated.)

### Mismatch diagnosis (normative; composes with the Error Contract)

When a `name@digest` reference resolves (through the one resolver) to a
store whose **current** config digest differs from the pinned digest, and
the **pinned config is recoverable** (so the pinned instance's uuid is in
hand — see the well-definedness note below), the resolver **MUST** turn
the one ambiguous "digest mismatch" into one of two precise diagnoses by
comparing the resolved store's uuid against the pinned instance's uuid:

- **same uuid** → *same instance, config evolved*. The reference still
  points at the right store; only its config moved on. The resolver
  **SHOULD** warn and treat it as a re-pin candidate, not a hard failure.
- **different uuid** → *wrong instance*. The name now resolves to a
  **different** physical store than the one pinned. This **MUST** be a
  hard error — it is the identity-layer form of the dodder#359 class,
  and silently proceeding is exactly the bug FDR-0010 exists to kill.

Where the pinned config is **not** recoverable, the resolver has no uuid
to compare against, so it **MUST** still fail fast on the plain digest
mismatch — a hard error, no uuid diagnosis, but never a silent proceed.

Example error text (different-uuid, hard error):

    error: blob-store pin "cache@blake2b256-2k4p9r3m…" resolved to the
            wrong instance
      name:     cache  (XDG user)
      pinned:   digest blake2b256-2k4p9r3m…  instance uuidv7-7q3w5h2x…
      resolved: digest blake2b256-9ft3m74l…  instance uuidv7-1a2b3c4d…
      cause:    the store now named "cache" is a DIFFERENT instance than
                the one pinned (uuid differs), not an evolved config
      action:   restore the pinned instance, or re-mint the reference
                against the intended store

**⚠ Concrete problem flagged** (per the design-review invitation, not a
relitigation): the diagnosis needs the **pinned instance's uuid**, but
the pin currency is a bare `name@digest`, which carries **no uuid**, and a
digest is not locally invertible to a uuid without the pinned config. So
the comparison is only well-defined when the resolver can obtain that
uuid, via one of:

1. a **uuid-bearing reference** — a documented *superset* of the digest
   currency (`name@digest` stays the default; instance-critical sites also
   record the target uuid). Robust, but it changes the currency decision.
2. a **recoverable pinned config** — a `multi` config's members are
   present at resolve time, so a member's uuid is readable directly
   (FDR-0009). Where the pinned config is in hand, no reference change is
   needed and the currency is untouched.
3. **inventory history** — a digest→uuid record letting madder attribute
   which uuid produced the pinned digest. No such record exists today.

The dodder-side revision (RFC-0007, `code.linenisgreat.com/dodder`)
selects **(2) as primary**, keeping the digest-only currency: uuid
mismatch-diagnosis applies wherever the pinned config is recoverable
(`multi` members), and **degrades to the plain fail-fast digest-mismatch
hard error** — no uuid comparison, still no silent proceed — where it is
not. **(1)** is recorded as a documented-but-not-adopted alternative: it
would change the currency, so it is a revisit-at-implementation item if
the degraded case proves common (persisted receipts are the candidate).
**(3)** is unjustified new machinery for now. This FDR aligns to that
choice so the two documents do not disagree on the currency.

### Terminology: name vs id

- The scoped human handle is the **name** — the blob-store name; the
  scope prefix (`.`/`//`/`%`/`~`/`_`) and dot-depth are part of the *name*
  grammar, not the id.
- The **uuid** is the **id** — the immutable instance identity.
- The digest is neither: it is a *config-version* fingerprint that,
  post-uuid, is instance-unique.
- The CLI **MAY** accept a shortest-distinct-prefix **digest
  abbreviation** in a `name@digest` reference (dodder's zettel-id
  abbreviation precedent). Abbreviation is a decoder/CLI-resolution
  concern, not grammar: `scoped_id.peg`'s `DigestSlot` already admits any
  `DataChar+` prefix (charset-strict, length-agnostic — piggy exports
  `DataChar` for exactly this), and the decoder resolves the prefix by
  length/checksum. The vector corpus pins an abbreviated digest as
  grammar-accept / parser-reject.

## Divergence Inventory

Where today's implementation diverges from the spec above. Each entry
names the exact function, what it does today, and the spec rule it
must converge to. These are the call sites the promotion criteria
require to be deleted or reduced to thin callers of the one resolver.

### 1. `env_dir.MakeDefaultAndInitialize` — `LocationTypeCwd` branch

`go/internal/echo/env_dir/construction.go`. Resolves a CWD id via
`resolveCwdAncestorOrError`: a **literal** Nth-parent walk with no
store-existence check, rooting the XDG at the raw ancestor. **Diverges**
from The Init Exception (init must create at `$PWD`, never derive a
location from dot-depth) and from the single-walk-up rule (the CWD walk
is store-aware, not literal). Under the spec this branch becomes: reject
`depth > 0`; for `depth == 0`, create at `$PWD`.

### 2. `directory_layout.ResolveNthAncestorMatch`

`go/internal/bravo/directory_layout/util.go`. The store-aware,
deepest-first, match-ranked *operate* walk. **This is the semantics the
spec adopts** as the one CWD walk-up — but today it is only one of two
CWD resolvers (the init path uses the literal walk instead), so the same
`..name` resolves differently depending on call site. Convergence:
`ResolveNthAncestorMatch` (or a successor with the same semantics)
becomes the *sole* CWD resolver, used by init and operate alike.

### 3. `env_dir.MakeWithHomeAndInitialize`

`go/internal/echo/env_dir/construction.go`. Pins to the user `$HOME`
XDG with no walk-up. This is the **correct** rule for a user-scoped id,
and current `MakeDefaultAndInitialize` already routes unprefixed init
here — that is the madder#227 fix (unprefixed init home-pins, matching
write; guarded by `TestInitBlobStore_UnprefixedIdIgnoresAncestorOverride`).
So this is **not a live madder read-bug** today. It appears in the
inventory because it is a *distinct constructor* rather than a branch of
the one resolver, and a walk-up-immune constructor reached alongside a
walk-up path is exactly how the disagreement class arises — the shape
still open in dodder as dodder#359 (`init-workspace`'s home-pinned parent
resolution vs the auto walk-up path). Convergence: user-scope resolution
stays home-pinned, but as the XDG-user *branch of the one resolver*, not
a separate constructor that another path can bypass.

### 4. `directory_layout.FindAllCwdOverridePaths`

`go/internal/bravo/directory_layout/util.go`. Collects **every**
ancestor `.madder/`, deepest-first. Legitimate as the enumeration
primitive behind `list` and behind `ResolveNthAncestorMatch`'s ranked
walk. **Diverges** only where its output is consumed as an implicit
*read-merge* substrate (via `makeAncestorOverrideStores` below).
Convergence: it survives as an enumeration/ranking helper; it **MUST
NOT** feed an implicit multi-store read view.

### 5. `blob_stores.MakeBlobStores` (and `makeAncestorOverrideStores`)

`go/internal/foxtrot/blob_stores/main.go`. Builds a live store for
every ancestor override, tags same-name collisions with ascending
cwd-depth, and merges the CWD-ancestor, XDG-user, and XDG-system sets
into one `BlobStoreMap` over which default-selection and
`blobFromRemainingStores` then operate. **Diverges** from Implicit
Union-Merge Is Retired: resolution reaches into a pre-merged cross-scope
map with fuzzy fallback instead of resolving each id independently to
one location. Convergence: `MakeBlobStores` may still *enumerate* for
`list`, but id→location resolution goes through the one resolver, and
the read-fallback set exists only for an explicit `multi` config.

## Conformance Suite

Tests are written **against this spec**, not against today's behaviour,
and use **non-`default`** store names throughout — the existing corpus
is almost entirely `default`, which is exactly the blind spot that let
spec and implementation drift silently (dodder#359 is a `default` bug).

- **Go** — `go/internal/echo/env_dir/resolution_conformance_test.go`
  (build tag `test`). Directly compares the literal init walk
  (`resolveCwdAncestorOrError`) against the store-aware operate walk
  (`directory_layout.ResolveNthAncestorMatch`) on the same trees, and
  pins the grammar→scope mapping in `scoped_id`.
- **bats** —
  `zz-tests_bats/resolution_conformance.bats`. Drives `madder init`,
  `madder info-repo <id> config-path` (the id→physical-location probe),
  `madder list`, and read/write with non-`default` names.
- **grammar** — `go/internal/alfa/scoped_id/scoped_id.peg` with the
  two-dimension corpus `go/internal/alfa/scoped_id/testdata/scoped_id_vectors.txt`.
  Each vector pins BOTH a `grammar` outcome (structure, under langlang)
  and a `parser` outcome (checksum/length/registration, under `Id.Set`).
  The parser half (`vectors_test.go`) runs today and is green; the
  grammar half runs as the `grammar-vectors` nix gate once langlang is
  wired as a hard dependency (flake-input-go_mod). This suite is
  all-green conformance — it has **no** EXPECTED-FAIL — because the
  grammar is sound; the vectors exist to keep grammar and decoder from
  drifting (e.g. an abbreviated or `uuidv7`-format pin is
  grammar-accept / parser-reject, and both harnesses must agree on that).

The two *resolution* suites (Go + bats) contain tests that **fail against
today's implementation** on purpose — they document a divergence. Each is
marked EXPECTED-FAIL (Go: `t.Skip` with a divergence annotation; bats:
`skip` with the same) naming the inventory entry it pins. Promotion to
`experimental` requires removing every EXPECTED-FAIL marker and having
those suites pass. Their PASS anchors (operate deepest-first; overflow
error shape; scope-from-prefix) establish that the spec's chosen
semantics already hold on the operate path and that the grammar is not
the defect.

## Rollout Sketch (non-normative)

1. Introduce `Resolve(id, cwd, env) (Location, error)` with the
   per-scope rules and the store-aware CWD walk.
2. Route `MakeDefaultAndInitialize`'s CWD branch through it: reject
   `depth > 0` at init; create at `$PWD` for `depth == 0`.
3. Route every operate call site through it; delete the literal
   `resolveCwdAncestorOrError` once it has no caller.
4. Remove the bare-name walk-up branch so user ids are home-pinned
   everywhere; keep `MakeWithHomeAndInitialize` as the user-scope path.
5. Stop consuming `makeAncestorOverrideStores` /
   `blobFromRemainingStores` as an implicit read-merge; keep
   `FindAllCwdOverridePaths` as an enumeration helper for `list`.
6. Flip each EXPECTED-FAIL in the conformance suite to a passing
   assertion as its divergence closes.

## More Information

Issue references: madder issues live on the Forgejo at
`code.linenisgreat.com/madder` (the `git@code.linenisgreat.com:madder.git`
origin; the GitHub `amarbel-llc/madder` mirror has issues disabled).
Bare `#N` denotes a madder issue; `dodder#N` a dodder issue
(`linenisgreat/dodder`).

- **Motivating incidents:**
  - **dodder#359** *(open)* — "default resolves to two different
    physical stores depending on code path." The cross-repo statement of
    the mechanism this FDR's one-resolver contract closes.
  - **#227** *(closed)* — the madder instance of the same class
    (unprefixed init lands in ancestor `.madder`, write resolves XDG).
    Fixed point-wise by home-pinning unprefixed init; this FDR
    generalizes that fix into the single-resolver contract so the class
    cannot recur on other paths.
- **Related dodder bugs the one-resolver contract closes:** dodder#283
  (nested same-named repo), dodder#341 (fresh env can't discover a
  pointer store), dodder#196 (clone split-brains lookups).
- **Cross-repo:** the dodder-side policy (repo auto-id discovery,
  `repos/<name>/` nesting, `/name` remote-first) is specified in the
  parallel revision of dodder's **FDR-0019**, which composes over this
  substrate. This FDR is the madder half of that redesign.
- **Depends on / composes with:**
  - **FDR-0009** (`docs/features/0009-multi-store-config-type.md`) —
    the explicit `multi` config that replaces implicit union-merge.
  - **FDR-0008** (`docs/features/0008-config-digest-pins.md`) — the
    digest-pinned member ids a `multi` config references.
- **The deliberate split this FDR dissolves:**
  `go/internal/echo/env_dir/AGENTS.md` and the header comments on
  `resolveCwdAncestorOrError` / `ResolveNthAncestorMatch` document the
  literal-vs-store-aware split as intentional. This FDR is the decision
  to stop maintaining two semantics.
- **Grammar:** `go/internal/alfa/scoped_id/scoped_id.peg` (Ford/langlang
  PEG, colocated with the parser, `@import`ing piggy's `marklid.peg`
  primitives per the one-home-per-grammar-unit ruling), the two-dimension
  corpus `.../testdata/scoped_id_vectors.txt`, and `vectors_test.go` (the
  parser half). Reference: `docs/man.7/blob-store.md`,
  `go/internal/alfa/scoped_id/main.go`, `go/internal/0/xdg_location_type`.
- **Instance identity (uuid):** the uuidv7 markl format is registered at
  madder's own site `go/internal/charlie/markl_registrations` — madder's
  first self-defined format, extending the madder#255 formats-live-in-piggy
  policy. That registration must be *activated* (imported) where consumers
  resolve ids, per **madder#278** (registrations are not pulled in
  transitively by the store packages — an in-process consumer hits a
  runtime `unknown format id` otherwise). Migration precedent: dodder's
  `migrate-repo-layout` (`code.linenisgreat.com/dodder`, dodder#363 — pure
  copy, source untouched). Composes with FDR-0008 (config digests become
  instance-unique) and FDR-0009 (`multi` members become instance pins).
- **langlang as a hard dependency:** the `grammar-vectors` gate requires
  langlang (the PEG tool), injected via `flake-input-go_mod` rather than a
  PATH-lookup + skip. Filed for hyphence (the grammar-vectors precedent)
  as `code.linenisgreat.com/hyphence` issue #13.
- **Walk-up / dot-depth lineage:** #153 (multi-dot `..name` init
  walk-up, parked), #145 (CWD-ancestor discovery walk-up), dodder#281
  (store-aware operate walk-up).
- **Legacy-migration tooling:** #175 *(open)* — improve the legacy
  `dodder-blob_store-config` rename error into a copy-pasteable command
  / `ErrorRetryable` in-place rename. #28 *(closed)* — the removed
  `migrate-legacy-configs` command. The resolver's legacy-error contract
  composes with #175; no separate tooling issue is filed by this FDR.

Signed-off-by: Clown 🤡 <https://github.com/amarbel-llc/clown>
