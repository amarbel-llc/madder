# env_dir multi-scope composition + struct-state refactor

Tracks GitHub issue [#123]. This document is *exploration only* — no
code changes proposed for adoption yet. The user asked for an ordered
breakdown plus a tracer consumer sketch; everything below is subject
to confirmation before implementation.

## What the code looks like today (verified by reading)

- `go/internal/echo/env_dir/construction.go` exposes
  `MakeDefault`, `MakeDefaultNoInit`, `MakeFromXDGDotenvPath`,
  `MakeDefaultAndInitialize`, `MakeWithDefaultHome`,
  `MakeWithXDGRootOverrideHomeAndInitialize`,
  `MakeWithHomeAndInitialize`, `MakeWithXDG`. Every constructor takes
  `(ctx, utilityName, debugOptions, opts ...Option)` plus
  constructor-specific extras. `Option` lives in
  `env_var_names.go` and today only carries `WithEnvVarNames`.
- `before_xdg.go:36` is the lone `os.Setenv` site. It writes
  `env.envVarNames.Binary` (default `BIN_MADDER`) to the process env at
  construction. There is already a `// TODO switch to useing
  MakeCommonEnv()` next to it.
- `main.go:138` and `main.go:218` define `AddToEnvVars` /
  `MakeCommonEnv`, both publishing the *same* `BIN_*` value via the
  env struct. **Neither method has any in-tree caller** (`rg
  '\.MakeCommonEnv\(|\.AddToEnvVars\(' --type go` returns no hits).
  So the on-disk subprocess contract is currently fed entirely through
  the `os.Setenv` side-effect; no exec.Cmd plumbing reads from the
  env struct yet.
- `go/internal/foxtrot/env_local/main.go` defines
  `env_local.Env` as the embedded union of one `env_ui.Env` and one
  `env_dir.Env`, constructed by `Make(ui, dir)`. `BlobStoreEnv`
  consumes `env_local.Env`, so the "one env per command" shape goes
  all the way down.
- `go/internal/golf/command_components/env_blob_store.go:73-82` is
  where `BlobStoreXDGScope` becomes the `utilityName` argument to
  `env_dir.MakeDefault`. cutting-garden's `capture.go:31` and
  `restore.go:24` set `BlobStoreXDGScope: "madder"`.

That last point is what the issue calls "the paper-over": cg
constructs *one* env_dir, with its scope set to madder's, and has no
way to also hold a cg-scoped env_dir alongside it.

## Tracer consumer candidate

The user picked "Add a tracer consumer." A minimal piece of
*actually-cg-only* state that exercises multi-scope without colliding
with madder's existing scopes:

> **`$XDG_STATE_HOME/cutting-garden/captures.log`** — append-only
> NDJSON record of `(timestamp, receipt-id, store-id, root-args)`
> emitted by `cutting-garden capture` after each receipt is written.

Why this specific candidate:

- Distinct from madder's `$XDG_LOG_HOME/madder/inventory_log/`, which
  is *per-blob* and naturally lives in madder's scope. cg's audit
  question is "what trees did I capture, where did the receipts
  land?" — that is operationally cg-shaped and has no equivalent
  blob-level signal.
- Capture is the only command that produces it; restore reads it
  not at all. So one command needs to hold *two* env_dir handles
  (madder's for blob stores, cg's for the log path) — exactly the
  multi-scope shape the issue describes.
- Small, write-only, and easy to delete if the tracer needs to be
  reshaped.

**Alternative considered:** restore staging dir under
`$XDG_CACHE_HOME/cutting-garden/restore-tmp-<pid>/` to enable
atomic-rename materialization. Rejected as the *first* tracer
because it changes restore's user-visible failure semantics (FDR
0001 §Limitations explicitly opts out of mid-stream rollback) and
would conflate the multi-scope refactor with a behavior change.

The captures.log tracer can land *after* the structural refactor —
its only job is to be a real consumer that proves multi-scope works.
If the team decides not to ship the log itself, swap in any other
cg-only state at the same seam.

## Ordered breakdown

Each step is independently revertable. Steps 1-3 are the structural
refactor; steps 4-5 add and consume the tracer. Risk notes call out
the load-bearing assumptions.

### Step 1 — `env_dir.Config` struct

Introduce a `Config` value carrying what `Make*` currently takes
positionally:

```go
type Config struct {
    XDGScope     string
    EnvVarNames  EnvVarNames     // defaults applied if zero
    DebugOptions debug.Options
    // future: PermitCwdXDGOverride, Initialize, etc.
}
```

Land the type, route every existing constructor through it
internally without changing exported signatures. Existing call sites
keep working; the type is purely additive at this step.

**Risk:** the existing `Option` pattern (only `WithEnvVarNames`
today) overlaps with `Config`. Decide before merging whether
`Option` becomes a layer over `Config` or is retired in favor of
"just pass a Config." Recommendation: retire `Option` once `Config`
covers its surface, but keep that change inside Step 1 so call sites
migrate exactly once.

### Step 2 — Drop `os.Setenv` at construction

Move `before_xdg.go:36` off `os.Setenv`. The binary path is already
on the env struct via `xdgInitArgs.ExecPath`; `MakeCommonEnv` /
`AddToEnvVars` already publish it. The fix is: remove the
`os.Setenv` call, then audit who *needs* the env var to be in
process env.

**Pre-condition that has to be verified before this step lands:**
nothing in this repo or in dewey reads `BIN_MADDER` (or whatever
`envVarNames.Binary` resolves to) via `os.Getenv`. A quick
`rg 'os\.Getenv\(.*BIN'` in this repo returned no hits, but I have
*not* checked dewey or any downstream consumer. If something out
there relies on inheritance, this step needs to either keep the
Setenv as opt-in (a `Config.WriteToProcessEnv bool`) or migrate
those readers to `exec.Cmd.Env` first.

**Why this is the right shape:** the issue's argument is that
construction-time process mutation makes multi-instance composition
fragile. Two env_dir instances each setting `BIN_MADDER` to
different values would race; today they happen to set the same
value, but the pattern is wrong. The struct-state path
(`MakeCommonEnv` consumed by explicit `exec.Cmd.Env`) gives every
spawn site control over which scope's binary it advertises.

### Step 3 — Multi-scope ergonomics

Once Step 1 has landed `Config` and Step 2 has removed the
process-env coupling, two env_dir instances in one process is just
"call `Make` twice with different `Config.XDGScope`." But the
*command-side* shape needs work:

- `command_components.EnvBlobStore` today produces an `env_local.Env`
  that bundles one ui + one dir. Either (a) extend `env_local.Env`
  to carry an optional secondary dir, or (b) hand the secondary
  dir to commands as a separate field on the command struct, not
  via env_local. Recommendation: (b) — env_local is "the single
  ambient env"; a multi-scope command has *two* envs and the
  asymmetry should be visible.
- Add a small helper on `EnvBlobStore` (or a sibling mixin) that
  builds the cg-scope env_dir from the same `req` and
  `DefaultConfig` plumbing, so both envs share `errors.Context`,
  `debug.Options`, and CustomOut/CustomErr.

**Risk:** I haven't traced every consumer of `env_local.Env` yet.
If callers pull `GetXDG()` off the local env and assume "this is
the *only* xdg in play," option (b) is safer; option (a) would
silently change which xdg they get. Need to grep
`env_local.Env`'s Get* surface before committing to either.

### Step 4 — Land the captures.log writer

Inside `commands_cutting_garden/capture.go`, after `sink.Finalize()`
succeeds:

1. Construct a cg-scoped env_dir via the Step-3 helper.
2. Resolve `$XDG_STATE_HOME/cutting-garden/captures.log` from it.
3. Append one NDJSON line per receipt produced
   `{ts, receipt_id, store_id, roots[]}`.

Wire `--no-capture-log` and `MADDER_CAPTURE_LOG=0` opt-outs only
if there's appetite — otherwise default-on, document in
`globals.go`'s `EnvVars` and `Files` blocks.

**Risk:** this introduces a new on-disk artifact that operators
will start depending on. If there's any chance the log format will
churn, mark it explicitly unstable in the man page (or hold this
step until the format is ratified). The structural refactor in
Steps 1-3 does *not* depend on this artifact landing — if the team
balks at the log itself, the structural work still has value.

### Step 5 — Pin the multi-scope contract with a test

Add a test that asserts:

1. `cutting-garden capture` writes a blob into a madder-scoped
   blob store *and* a line into a cg-scoped captures.log — proving
   both env_dir instances are addressing disjoint XDG scopes.
2. Setting `MADDER_XDG_UTILITY_OVERRIDE` does *not* redirect
   cg's captures.log (cg's env_dir uses its own override env var,
   which Step 1's Config makes per-instance).

This is the load-bearing test for "scopes cannot affect one
another," which is the architectural intent the issue states.

## What I deliberately did not do

- **Did not commit to the `Option` retirement.** Step 1 sketches it
  but the call sites that pass `WithEnvVarNames` today need to be
  enumerated and migrated atomically; that's worth a separate
  pre-step if the surface is wider than it looks.
- **Did not verify dewey-side `BIN_MADDER` consumers.** Step 2's
  pre-condition is real and unchecked.
- **Did not write a migration story for downstream of madder.** If
  anyone is importing `env_dir` as a library and relying on
  construction-time `os.Setenv`, Step 2 is breaking. Need to
  decide whether that's a v0.x freedom or warrants a deprecation
  cycle.

## Open questions for the user

1. Tracer scope: `$XDG_STATE_HOME/cutting-garden/captures.log` as
   sketched, or pick a different cg-only artifact? Restore staging
   dir is the obvious alternative.
2. `Option` retirement during Step 1, or hold it for a follow-up?
3. Is the `BIN_MADDER` env-var audit (Step 2 pre-condition) a
   blocker, or do we accept that downstream readers may need a
   parallel migration?

[#123]: https://github.com/amarbel-llc/madder/issues/123
