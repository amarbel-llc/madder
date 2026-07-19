# Extract cutting-garden into `amarbel-llc/cutting-garden` — design

Date: 2026-05-10
Status: design (pre-approved by user; awaiting implementation plan)
Supersedes the placeholder reference in
`/home/sasha/eng/repos/madder/.worktrees/rich-hazel/CLAUDE.md`
(the line "The extraction plan is recorded at
`docs/plans/2026-05-02-extract-cutting-garden.md`" — that file does
not exist; this doc replaces it).

## Goal

Stand up a new repository `github.com/amarbel-llc/cutting-garden`
containing a clean reimplementation of cutting-garden on top of
madder's public `pkgs/` substrate, plus a small command framework
ported from `amarbel-llc/dodder` HEAD. Mature the framework there in
isolation. Madder's current `go/internal/india/commands_cutting_garden/`
stays untouched and continues to ship `cutting-garden` from inside
madder; the standalone repo is additive until it reaches feature
parity.

## Motivation and history

This design landed after several scope pivots in a single brainstorm:

1. The brainstorm started with closing a specific shell-completion gap
   for blob-store-id positional args (issue
   [#161](https://github.com/amarbel-llc/madder/issues/161)). Both the
   legacy `complete` subcommand and the newer `__complete` subcommand
   in `go/internal/futility/` are unable to fire blob-store-id
   completion today; three call sites
   (`golf/command_components.GetFlagValueBlobIds`,
   `commands.Sync.Complete`,
   `commands_cutting_garden.Capture.Complete`) are stranded.

2. Investigating the gap surfaced that madder's `futility` framework
   has structurally drifted from the `dodder` framework it was
   originally extracted from, and that the older dodder design is a
   better fit for madder's actual completion grammar (variadic args
   that interleave shapes — markl-IDs, paths, blob-store-ids — within
   a single positional list).

3. We weighed three migration shapes for adopting dodder HEAD's
   framework on the madder side: atomic flag-day, dual-architecture
   with shared infrastructure made polymorphic, and dual-architecture
   with `command_components` forked. Each had material cost; the
   `command_components` plumbing turned out to be more entangled with
   `futility.Request` than the polymorphism story could absorb cleanly
   in Phase 1.

4. The user proposed extracting cutting-garden into its own repo and
   maturing the framework there in isolation. Cutting-garden is small
   enough (capture, restore, diff) for a clean rewrite, and madder's
   `pkgs/` substrate was explicitly designed for exactly this case —
   madder's CLAUDE.md already names "future wrappers" as the
   intended consumers of `pkgs/env_dir`, `pkgs/madder_env`,
   `pkgs/blob_store_env`, `pkgs/env_local`, and `pkgs/env_ui`.

cutting-garden becomes the first public consumer of that substrate.
Rough edges in `pkgs/` discovered during the port become upstream
issues filed against madder.

## What this design does NOT do

- Touch `go/internal/futility/`. It stays as-is.
- Touch `go/internal/india/commands_cutting_garden/`. Madder continues
  to build the `cutting-garden` binary from there.
- Touch `go/internal/golf/command_components/`. No fork, no
  polymorphism refactor.
- Resolve issue
  [#161](https://github.com/amarbel-llc/madder/issues/161) (blob-store-id
  completion) on the madder side. The new-repo cutting-garden ships
  with working completion as a side effect of using the new framework;
  the madder side keeps the gap until futility is migrated separately.
- Drop or relocate MCP support. `madder-mcp` is unaffected.

## Architecture

### Module identity

- Repository: `github.com/amarbel-llc/cutting-garden` (new, empty
  repo on GitHub).
- Go module path: `github.com/amarbel-llc/cutting-garden`.
- Local working tree: `~/eng/repos/cutting-garden` (peer of
  `~/eng/repos/madder`).

### Repo layout (initial)

```
cutting-garden/
├── go.mod                       # module github.com/amarbel-llc/cutting-garden
├── go.sum
├── flake.nix                    # builds binary + man pages + completion stubs
├── cmd/
│   └── cutting-garden/main.go
├── internal/
│   ├── command/                 # ported from dodder HEAD; matures here
│   ├── commands/                # capture, restore, diff (Phase 2+)
│   └── command_components/      # cg-side env wiring atop madder pkgs (Phase 2+)
├── docs/
│   ├── man.1/cutting-garden.md
│   ├── man.7/{capture-receipt,blob-store}.md
│   └── rfcs/0003-capture-restore-rules.md
└── README.md
```

`internal/command_components` may turn out to be unnecessary if
madder's `pkgs/blob_store_env` exposes everything cutting-garden's
commands need without a mixin. Phase 2's first concrete port (capture)
will resolve that question.

### Library dependency on madder

cutting-garden imports madder's public substrate at
`code.linenisgreat.com/madder/go/pkgs/{env_dir, env_local, env_ui,
blob_store_env, madder_env}`. Versioned via Go module semantics —
cutting-garden pins a release tag of madder rather than tracking
master.

cutting-garden does NOT import any `internal/` package from madder.
This is the pkgs/internal boundary madder already enforces and is the
test for whether `pkgs/` is sufficient.

### Framework: ported from dodder HEAD

Dodder's `go/internal/delta/command/` package is the template. The
port preserves dodder's public surface and adds three madder-derived
pieces (manpage gen, completion-stub gen, repaired positional
dispatch).

#### Phase 1 file inventory

| File | Source | Notes |
| --- | --- | --- |
| `cmd.go` | port from dodder HEAD | `Cmd { Run(Request) }`, `Description`, `CommandWithDescription` |
| `arg.go` | port from dodder HEAD | `Arg`, `ArgGroup`, `CommandWithArgs`, `MCPAnnotations`, `CommandWithMCPAnnotations` (kept for future MCP use; harmless dead code now) |
| `request.go` | port from dodder HEAD | `Request{Utility, Context, FlagSet, input}`, `PopArg`/`PopArgs`/`PeekArgs`/`LastArg`/`RemainingArgCount` |
| `command_line_input.go` | port from dodder HEAD | `CommandLineInput{FlagsOrArgs, InProgress, ContainsDoubleHyphen, Args, Argi}` |
| `completion.go` | port from dodder HEAD | `Completer`, `Completion`, `FuncCompleter`, `FlagValueCompleter` |
| `utility.go` | port from dodder HEAD | `Utility`, `MakeUtility`, `AddCmd`, `GetCmd`, `AllCmds`, `MergeUtility`, `Run`, `MakeCmdAndFlagSet`, `MakeRequest` |
| `utility_run.go` | port from dodder HEAD | `handleMainErrors`, `extendNameIfNecessary` |
| `complete.go` | port + repair | The visible `complete` subcommand with **working positional arg dispatch**. Dispatches via `cmd.(Completer)` against the registered instance directly (no `*Command` wrapper indirection). |
| `manpage.go` | new (shim) | `EnvVar`, `FilePath`, `ManpageFile`, `Example` types + opt-in interfaces (`CommandWithEnvVars`/`CommandWithFiles`/`CommandWithSeeAlso`/`CommandWithExamples`/`CommandWithManpageFiles`) |
| `generate_manpages.go` | port + adapt from `futility/generate.go` | Reads `Utility.AllCmds()` + opt-in interfaces; no `*Command` indirection |
| `generate_completions.go` | new, minimal | Emits ~30-line stub per shell that delegates to `<binary> complete --bash-style …` |

#### Cmd interface (target shape)

The interface is intentionally tiny:

```go
type Cmd interface {
    Run(Request)
}
```

All metadata is **opt-in via feature interfaces** on the command
instance:

- `CommandWithDescription { GetDescription() Description }` — short/long
  for manpages and `--help`.
- `CommandWithArgs { GetArgs() []ArgGroup }` — declarative positional
  args for SYNOPSIS, ARGUMENTS, and (future) MCP schema.
- `CommandWithEnvVars`, `CommandWithFiles`, `CommandWithSeeAlso`,
  `CommandWithExamples`, `CommandWithManpageFiles` — the manpage-gen
  shim.
- `CommandWithMCPAnnotations` — kept for future MCP work; inert in
  Phase 1.
- `interfaces.CommandComponentWriter` (from dewey) —
  `SetFlagDefinitions(*flags.FlagSet)` for direct Go-style flag
  wiring.
- `Completer { Complete(Request, any, CommandLineInput) }` — for
  positional + flag-value completion.

Manpage and completion generators discover capabilities by
type-asserting against each registered instance, which removes the
extracted-metadata `*Command` wrapper struct that futility currently
maintains.

#### `Completer` second-arg type: `any`, not `env_local.Env`

Dodder's `command.Completer` takes a concrete
`env_local.Env` from `madder/go/pkgs/env_local`. To keep the new repo's
`command` package portable to future wrapper utilities (and to avoid
circularity between the framework and any specific env type), the
new-repo Completer signature uses `any` for the second arg:

```go
type Completer interface {
    Complete(Request, any, CommandLineInput)
}
```

cutting-garden commands type-assert the `any` to `env_local.Env` at
the call site, exactly as madder's
`go/internal/india/commands/complete.go:73` already does. This is a
deliberate departure from dodder HEAD's tighter typing in service of
the framework's reuse story.

### Build and release

- `flake.nix` modeled on madder's: builds the `cutting-garden` binary,
  runs `generate_manpages` to produce `share/man/man{1,7}/*`, runs
  `generate_completions` to produce shell stubs under
  `share/{bash-completion/completions,zsh/site-functions,fish/vendor_completions.d}`.
- Versioning starts at `v0.1.0` once Phase 2 reaches feature parity.
  Phase 1's framework-only commit is `v0.0.x` pre-release.

## Phasing

### Phase 1 — framework only

**Single commit / first push to the new repo:**

1. Create empty `amarbel-llc/cutting-garden` GitHub repo.
2. Initialize local clone at `~/eng/repos/cutting-garden`.
3. Scaffold `go.mod`, `flake.nix` (minimal), `README.md`, `.gitignore`.
4. Port dodder HEAD's `command` package per the file inventory above.
5. Add the manpage-gen shim and stub-style completion gen.
6. Repair the positional dispatch in the `complete` subcommand.
7. Unit tests covering `Utility`, `Request`, completion dispatch, and
   manpage gen against a fixture command.
8. CI: minimal GitHub Actions workflow running `go test ./...` and
   `nix build`.

No commands ported; no `cmd/cutting-garden/main.go` yet (or just a
stub that runs `command.MakeUtility("cutting-garden", ...).Run(args)`
with no commands registered, to prove the framework boots).

### Phase 2 and beyond — command ports

Out of scope for this design doc; outline only:

- Phase 2: port `capture` end-to-end with bats parity.
- Phase 3: port `restore`.
- Phase 4: port `diff`.
- Phase 5: documentation (man.1/man.7, README, RFC 0003).
- Phase N: feature-parity declared; tag `v0.1.0`; promotion review
  starts.

Each phase lands as its own design doc + plan.

## Testing strategy (Phase 1)

- **Unit tests** in `internal/command/`:
  - `Utility.AddCmd` panics on duplicate name.
  - `MergeUtilityWithPrefix` namespaces correctly.
  - `Run` with no args prints usage and cancels.
  - `Run` with unknown subcommand prints usage and cancels.
  - `MakeRequest` builds a `Request` with the expected `FlagSet`,
    `Context`, and `CommandLineInput` shape.
- **Completion dispatch tests**:
  - Bare `complete` invocation lists registered subcommands.
  - `complete <subcmd>` (with no value being completed) calls
    `subcmd.Complete` if the registered instance implements
    `Completer`.
  - `complete <subcmd>` against a non-Completer command does not panic
    and prints nothing.
  - Flag-value completion still works via the
    `flagValue.Complete(req, env, commandLine)` path (the path that's
    already correct in madder's legacy `complete.go`).
- **Manpage gen golden test**: a fixture command implementing
  `CommandWithDescription` + `CommandWithEnvVars` produces an expected
  `.1` man source. Asserts that the gen reads the live instance, not
  any cached metadata struct.
- **Stub gen test**: emits the expected ~30-line bash/fish/zsh stubs
  that delegate to `<binary> complete --bash-style …`.
- **CI gating**: `go test ./...`, `go vet ./...`, `nix build`.

## Rollback

This is an additive change: a new repo with no callers in the existing
ecosystem. There is no rollback in the conventional sense.

- If the framework design proves wrong, abandon: archive or delete the
  GitHub repo; remove the local clone; nothing else changes.
- If a specific commit on the new repo introduces a regression,
  standard `git revert` applies; no cross-repo coordination needed.
- The madder side's `cutting-garden` binary continues to ship from
  `go/internal/india/commands_cutting_garden/` regardless of what
  happens in the new repo. Users who want stability stay on madder's
  build; users who want the new framework opt in by installing the
  new-repo build.

### Promotion criteria — when to consider "done"

- Phase 2+ ports capture, restore, diff with full bats parity.
- Standalone cutting-garden runs in production for ≥7 days with no
  functional or UX regression vs. the madder-side build.
- Public substrate (`madder/go/pkgs/`) gaps surfaced during the port
  are filed as madder issues and resolved (or explicitly accepted as
  permanent).
- Framework battle-tested on this small surface: completion fires
  correctly for blob-store-id positional args; manpages render
  correctly; build works on Linux + macOS.

When all met, the next decision (separate brainstorm) is whether to
adopt the matured framework on the madder side as the futility
replacement, and whether to delete the madder-side cutting-garden
sources in favor of pinning the standalone build as the supported
distribution.

## Affected open issues

- [#160 — `madder list` show XDG location of each blob store](https://github.com/amarbel-llc/madder/issues/160)
  — independent; remains a madder-side change.
- [#161 — blob-store-id shell completion is unreachable](https://github.com/amarbel-llc/madder/issues/161)
  — the new-repo cutting-garden side ships with the gap closed (via
  the repaired `complete` subcommand). The madder side keeps the gap;
  resolved later by the futility migration or by independently
  repairing the legacy path. Note this in the issue's triage.
- [#162 — cutting-garden capture: clean directory paths](https://github.com/amarbel-llc/madder/issues/162)
  — bug; should be fixed on the madder side first since that's what
  ships today, then carried forward when capture is ported in Phase 2.

## Open items deferred to the implementation plan

- Whether `internal/command_components` is needed in the new repo, or
  whether commands consume `madder/go/pkgs/blob_store_env` directly.
  Resolved when capture is ported in Phase 2.
- The exact MCP shape for cutting-garden, when MCP is restored.
- Versioning cadence for cutting-garden's pin of madder.
- Whether the new repo's completion grammar should ever support
  flag-shell-completion via the `__complete`-style path (the new repo
  uses only the `complete` subcommand path; if a future need surfaces,
  re-evaluate then).
