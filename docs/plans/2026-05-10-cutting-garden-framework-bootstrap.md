# cutting-garden Framework Bootstrap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use eng:subagent-driven-development to implement this plan task-by-task.

**Goal:** Stand up an empty `github.com/amarbel-llc/cutting-garden` repo
containing only the `command` package (ported from
`amarbel-llc/dodder` HEAD), repaired completion dispatch, manpage and
stub-style completion generators, and a minimal `cutting-garden`
binary that boots the framework with zero registered commands. Phase 2+
command ports (capture, restore, diff) are out of scope.

**Architecture:** Single Go module at
`github.com/amarbel-llc/cutting-garden`. The `command` package is a
clean port of dodder's `go/internal/delta/command/` with three
madder-derived additions: a manpage-gen shim (typed opt-in interfaces
on each command instance), a stub-style completion gen (emits ~30-line
shell stubs that delegate to `<binary> complete --bash-style …`), and
a repaired `complete` subcommand whose positional-arg dispatch
actually invokes `cmd.(Completer).Complete(...)` against the
registered instance. No commands ported in Phase 1.

**Tech Stack:**
- Go 1.23+ (matching madder)
- `code.linenisgreat.com/purse-first/libs/dewey/...` for shared
  primitives (`charlie/flags`, `bravo/errors`, `charlie/ui`,
  `charlie/values`, `0/interfaces`, etc.)
- Nix flake for builds (modeled on madder's `flake.nix`)
- `pivy-agent` for GPG-signed commits (already configured for the user)
- GitHub Actions for CI (Go test + Nix build)

**Rollback:** N/A. Purely additive — a new repo with no callers.
Abandonment = archive the GitHub repo and delete the local clone.

**Reference design:**
`/home/sasha/eng/repos/madder/.worktrees/rich-hazel/docs/plans/2026-05-10-extract-cutting-garden-design.md`

**Reference dodder source** (for the port):
`https://github.com/amarbel-llc/dodder/tree/HEAD/go/internal/delta/command/`

---

## Pre-flight

These checks must pass before starting Task 1:

- [ ] You can run `gh auth status` and see an authenticated user with
      access to the `amarbel-llc` org.
- [ ] `pivy-agent` is unlocked (try `git -c user.signingkey=<key> commit
      --allow-empty -m test` from any repo; if it fails with
      "Inappropriate ioctl for device" or similar, ask the user to
      unlock pivy-agent before continuing).
- [ ] `~/eng/repos/cutting-garden/` does NOT exist yet (verify with
      `test -e ~/eng/repos/cutting-garden && echo EXISTS || echo OK`).

If any pre-flight fails, stop and report to the user.

---

## Import substitution table

Throughout the port, dodder-specific imports become dewey or stdlib:

| dodder import | new-repo substitution |
| --- | --- |
| `code.linenisgreat.com/dodder/go/lib/0/collections_slice` | `code.linenisgreat.com/purse-first/libs/dewey/bravo/collections_slice` |
| `code.linenisgreat.com/dodder/go/lib/alfa/flags` | `code.linenisgreat.com/purse-first/libs/dewey/charlie/flags` |
| `code.linenisgreat.com/dodder/go/lib/alfa/ui` | `code.linenisgreat.com/purse-first/libs/dewey/charlie/ui` |
| `code.linenisgreat.com/dodder/go/lib/alfa/quiter` | `code.linenisgreat.com/purse-first/libs/dewey/alfa/quiter` (verify exists; otherwise drop usage) |
| `code.linenisgreat.com/dodder/go/lib/charlie/config_cli` | `code.linenisgreat.com/purse-first/libs/dewey/foxtrot/config_cli` |
| `code.linenisgreat.com/dodder/go/lib/bravo/cli` | drop the `CLICompleter` alias for Phase 1 (not needed) |
| `code.linenisgreat.com/purse-first/libs/dewey/0/interfaces` | unchanged |
| `code.linenisgreat.com/purse-first/libs/dewey/bravo/errors` | unchanged |
| `code.linenisgreat.com/madder/go/pkgs/env_local` | **drop** — use `any` for the second `Completer` arg per the design doc |

If a substituted import doesn't exist or has drifted, run
`go doc code.linenisgreat.com/purse-first/libs/dewey/...` (or
`hamster.doc`) to find the right path before asking for help.

---

## Task 1: Create the GitHub repo

**Promotion criteria:** N/A (one-shot setup).

**Files:** None local yet.

**Step 1: Create empty repo via GitHub API**

```bash
gh repo create amarbel-llc/cutting-garden \
  --private \
  --description "Filesystem-tree capture/restore CLI atop madder's blob store"
```

Expected output: `https://github.com/amarbel-llc/cutting-garden`.

If `gh repo create` fails with "Name already exists", confirm with the
user that it's their repo and we can proceed; otherwise stop.

**Step 2: Verify the repo exists**

```bash
gh repo view amarbel-llc/cutting-garden --json name,visibility,description
```

Expected: a JSON object with `"name": "cutting-garden"`,
`"visibility": "PRIVATE"`, and the description from Step 1.

---

## Task 2: Scaffold local clone

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/.git/` (via clone)

**Step 1: Clone the empty repo**

```bash
cd ~/eng/repos
gh repo clone amarbel-llc/cutting-garden
cd cutting-garden
```

Expected: an empty directory with only `.git/`.

**Step 2: Verify the working tree is empty**

```bash
ls -A1
```

Expected output: only `.git`.

---

## Task 3: Initialize go.mod and .gitignore

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/go.mod`
- Create: `~/eng/repos/cutting-garden/.gitignore`
- Create: `~/eng/repos/cutting-garden/README.md`

**Step 1: Initialize go module**

```bash
cd ~/eng/repos/cutting-garden
go mod init github.com/amarbel-llc/cutting-garden
```

Expected: `go.mod: creating new go.mod: module github.com/amarbel-llc/cutting-garden`.

**Step 2: Write .gitignore**

```
# Build artifacts
result
result-*
.direnv/

# Editor noise
.vscode/
.idea/
*.swp
*.swo

# Test artifacts
*.test
*.prof
coverage.out
```

**Step 3: Write README.md**

```markdown
# cutting-garden

Filesystem-tree capture/restore CLI built on top of
[madder](https://github.com/amarbel-llc/madder)'s blob store.

## Status

Phase 1 — framework bootstrap. No commands implemented yet. See
[the extraction design](https://github.com/amarbel-llc/madder/blob/master/docs/plans/2026-05-10-extract-cutting-garden-design.md)
in the madder repo for context.

## Build

```bash
nix build
```

## Test

```bash
go test ./...
```
```

**Step 4: Commit**

```bash
cd ~/eng/repos/cutting-garden
git add go.mod .gitignore README.md
git commit -S -m "$(cat <<'EOF'
chore: initial scaffold

Empty Go module with .gitignore and README. Framework port lands in
subsequent commits per
docs/plans/2026-05-10-extract-cutting-garden-design.md in madder.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:
EOF
)"
```

Expected: signed commit with `[master <sha>] chore: initial scaffold`.

---

## Task 4: Add minimal CI

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/.github/workflows/ci.yml`

**Step 1: Write workflow file**

```yaml
name: ci

on:
  push:
    branches: [master]
  pull_request:

jobs:
  go-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - run: go test ./...

  go-vet:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - run: go vet ./...
```

(We add `nix build` to CI later in Task 16 once `flake.nix` exists.)

**Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -S -m "ci: add go test and go vet workflows

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 5: Port `command/cmd.go`

**Promotion criteria:** N/A (no old approach).

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/cmd.go`
- Create: `~/eng/repos/cutting-garden/internal/command/cmd_test.go`

**Step 1: Write the failing test**

`internal/command/cmd_test.go`:

```go
package command

import "testing"

type fakeCmd struct {
	ran bool
}

func (c *fakeCmd) Run(req Request) {
	c.ran = true
}

func (fakeCmd) GetDescription() Description {
	return Description{Short: "fake command for tests"}
}

func TestCmd_RunIsCallable(t *testing.T) {
	var c fakeCmd
	c.Run(Request{})
	if !c.ran {
		t.Error("Run was not invoked")
	}
}

func TestDescription_Fields(t *testing.T) {
	d := Description{Short: "short", Long: "long"}
	if d.Short != "short" || d.Long != "long" {
		t.Errorf("Description fields not preserved: %+v", d)
	}
}

func TestCommandWithDescription_Implementable(t *testing.T) {
	var c CommandWithDescription = fakeCmd{}
	if c.GetDescription().Short != "fake command for tests" {
		t.Error("CommandWithDescription not implemented as expected")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
cd ~/eng/repos/cutting-garden
go test ./internal/command/ -run 'TestCmd_RunIsCallable|TestDescription_Fields|TestCommandWithDescription_Implementable' -v
```

Expected: FAIL with "undefined: Request" (or similar — the package
doesn't compile yet).

**Step 3: Write minimal implementation**

`internal/command/cmd.go`:

```go
package command

type (
	Cmd interface {
		Run(Request)
	}

	Description struct {
		Short, Long string
	}

	CommandWithDescription interface {
		GetDescription() Description
	}
)
```

This still won't compile because `Request` doesn't exist yet. Define a
**stub** `Request` in cmd.go for now (it gets replaced in Task 7):

Add to `internal/command/cmd.go`:

```go
// Request stub — full type lands in Task 7.
type Request struct{}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/command/ -v
```

Expected: PASS for all three tests.

**Step 5: Commit**

```bash
git add internal/command/cmd.go internal/command/cmd_test.go
git commit -S -m "feat(command): add Cmd, Description, CommandWithDescription

Ports go/internal/delta/command/cmd.go from amarbel-llc/dodder HEAD.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 6: Port `command/arg.go`

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/arg.go`
- Create: `~/eng/repos/cutting-garden/internal/command/arg_test.go`

**Step 1: Write the failing test**

`internal/command/arg_test.go`:

```go
package command

import (
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/charlie/values"
)

func TestArg_FieldsPreserved(t *testing.T) {
	a := Arg{
		Name:        "blob-store-id",
		Description: "store id",
		Required:    true,
		Variadic:    true,
		EnumValues:  []string{"a", "b"},
		Value:       &values.String{},
	}
	if a.Name != "blob-store-id" {
		t.Errorf("Name = %q, want blob-store-id", a.Name)
	}
	if !a.Required || !a.Variadic {
		t.Error("Required/Variadic not preserved")
	}
}

type fakeArgsCmd struct{}

func (fakeArgsCmd) Run(req Request) {}

func (fakeArgsCmd) GetArgs() []ArgGroup {
	return []ArgGroup{{
		Name: "primary",
		Args: []Arg{{Name: "id", Required: true}},
	}}
}

func TestCommandWithArgs_Implementable(t *testing.T) {
	var c CommandWithArgs = fakeArgsCmd{}
	if got := len(c.GetArgs()); got != 1 {
		t.Errorf("GetArgs() len = %d, want 1", got)
	}
}

func TestMCPAnnotations_Fields(t *testing.T) {
	m := MCPAnnotations{ReadOnly: true, Destructive: false}
	if !m.ReadOnly || m.Destructive {
		t.Errorf("MCPAnnotations field roundtrip failed: %+v", m)
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run 'TestArg|TestCommandWithArgs|TestMCPAnnotations' -v
```

Expected: FAIL with "undefined: Arg" or similar.

**Step 3: Write implementation**

`internal/command/arg.go` — ported verbatim from dodder HEAD with the
import substitution from the table:

```go
package command

import "code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"

type (
	Arg struct {
		Name        string
		Description string
		Required    bool
		Variadic    bool
		EnumValues  []string
		Value       interfaces.FlagValue
	}

	ArgGroup struct {
		Name        string
		Description string
		Args        []Arg
	}

	CommandWithArgs interface {
		GetArgs() []ArgGroup
	}

	MCPAnnotations struct {
		ReadOnly    bool
		Destructive bool
	}

	CommandWithMCPAnnotations interface {
		GetMCPAnnotations() MCPAnnotations
	}
)
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS for all arg tests + the cmd tests still pass.

**Step 5: Commit**

```bash
git add internal/command/arg.go internal/command/arg_test.go
git commit -S -m "feat(command): add Arg, ArgGroup, CommandWithArgs, MCPAnnotations

Ports go/internal/delta/command/arg.go from amarbel-llc/dodder HEAD.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 7: Port `command/command_line_input.go` and `command/request.go`

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/command_line_input.go`
- Create: `~/eng/repos/cutting-garden/internal/command/command_line_input_test.go`
- Modify: `~/eng/repos/cutting-garden/internal/command/cmd.go` (remove
  the `Request` stub)
- Create: `~/eng/repos/cutting-garden/internal/command/request.go`
- Create: `~/eng/repos/cutting-garden/internal/command/request_test.go`

These two ship together because `Request` references `CommandLineInput`.

**Step 1: Write the failing test for CommandLineInput**

`internal/command/command_line_input_test.go`:

```go
package command

import (
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/bravo/collections_slice"
)

func TestCommandLineInput_LastArg(t *testing.T) {
	cli := CommandLineInput{FlagsOrArgs: collections_slice.String{"a", "b", "c"}}
	arg, ok := cli.LastArg()
	if !ok || arg != "c" {
		t.Errorf("LastArg = (%q, %v), want (c, true)", arg, ok)
	}
}

func TestCommandLineInput_LastArg_Empty(t *testing.T) {
	cli := CommandLineInput{}
	_, ok := cli.LastArg()
	if ok {
		t.Error("LastArg on empty FlagsOrArgs returned ok=true")
	}
}

func TestCommandLineInput_LastCompleteArg_StripsInProgress(t *testing.T) {
	cli := CommandLineInput{
		FlagsOrArgs: collections_slice.String{"a", "b", "in-prog"},
		InProgress:  "in-prog",
	}
	arg, ok := cli.LastCompleteArg()
	if !ok || arg != "b" {
		t.Errorf("LastCompleteArg = (%q, %v), want (b, true)", arg, ok)
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run TestCommandLineInput -v
```

Expected: FAIL with "undefined: CommandLineInput".

**Step 3: Write CommandLineInput**

`internal/command/command_line_input.go` (port from dodder; substitute
the collections_slice import):

```go
package command

import (
	"fmt"

	"code.linenisgreat.com/purse-first/libs/dewey/bravo/collections_slice"
)

// TODO complete merging Args, consumed and FlagsOrArgs for use by Run/Complete
type CommandLineInput struct {
	FlagsOrArgs          collections_slice.String
	InProgress           string
	ContainsDoubleHyphen bool

	Args collections_slice.String
	Argi int

	consumed collections_slice.Slice[consumedArg]
}

type consumedArg struct {
	name, value string
}

func (arg consumedArg) String() string {
	if arg.name == "" {
		return fmt.Sprintf("%q", arg.value)
	}
	return fmt.Sprintf("%s:%q", arg.name, arg.value)
}

func (commandLine CommandLineInput) LastArg() (arg string, ok bool) {
	argc := commandLine.FlagsOrArgs.Len()
	if argc > 0 {
		ok = true
		arg = commandLine.FlagsOrArgs.Last()
	}
	return arg, ok
}

func (commandLine CommandLineInput) LastCompleteArg() (arg string, ok bool) {
	argc := commandLine.FlagsOrArgs.Len()
	if commandLine.InProgress != "" {
		argc -= 1
	}
	if argc > 0 {
		ok = true
		arg = commandLine.FlagsOrArgs.Last()
	}
	return arg, ok
}
```

**Note:** `LastCompleteArg` has a known bug in dodder — when `InProgress`
matches the last element, it should index `[argc-1]` (after decrement),
not `Last()`. Carry the bug forward in this port (matching dodder
exactly is the goal); file an upstream issue for both repos as Task 17.

**Step 4: Verify CommandLineInput tests pass and Request tests still need writing**

```bash
go test ./internal/command/ -run TestCommandLineInput -v
```

Expected: PASS for the LastArg test. The LastCompleteArg test as
written above will FAIL because of the dodder bug. **Mark that test as
skipped with a TODO referencing the upstream bug**, then re-run:

```go
func TestCommandLineInput_LastCompleteArg_StripsInProgress(t *testing.T) {
	t.Skip("dodder upstream bug; see Task 17 follow-up")
	// ... rest of test ...
}
```

Re-run, expect PASS for everything.

**Step 5: Write the failing Request test**

`internal/command/request_test.go`:

```go
package command

import (
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/bravo/collections_slice"
	"code.linenisgreat.com/purse-first/libs/dewey/charlie/flags"
)

func newTestRequest(args ...string) Request {
	fs := flags.NewFlagSet("test", flags.ContinueOnError)
	_ = fs.Parse(args)
	return Request{
		FlagSet: fs,
		input: &CommandLineInput{
			FlagsOrArgs: collections_slice.String(args),
			Args:        collections_slice.String(args),
		},
	}
}

func TestRequest_RemainingArgCount(t *testing.T) {
	req := newTestRequest("a", "b", "c")
	if got := req.RemainingArgCount(); got != 3 {
		t.Errorf("RemainingArgCount = %d, want 3", got)
	}
}

func TestRequest_PopArg(t *testing.T) {
	req := newTestRequest("alpha", "beta")
	got := req.PopArg("first")
	if got != "alpha" {
		t.Errorf("PopArg = %q, want alpha", got)
	}
	if req.RemainingArgCount() != 1 {
		t.Errorf("RemainingArgCount after Pop = %d, want 1",
			req.RemainingArgCount())
	}
}

func TestRequest_PeekArgs(t *testing.T) {
	req := newTestRequest("a", "b")
	peek := req.PeekArgs()
	if len(peek) != 2 || peek[0] != "a" || peek[1] != "b" {
		t.Errorf("PeekArgs = %v, want [a b]", peek)
	}
	if req.RemainingArgCount() != 2 {
		t.Error("PeekArgs mutated remaining args")
	}
}
```

**Step 6: Remove the `Request{}` stub from cmd.go and write the real Request**

Edit `internal/command/cmd.go` to remove the `type Request struct{}`
stub.

`internal/command/request.go` (port from dodder; substitute imports):

```go
package command

import (
	"slices"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/collections_slice"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/charlie/flags"
)

type Request struct {
	Utility Utility
	Context interfaces.ActiveContext
	FlagSet *flags.FlagSet
	input   *CommandLineInput
}

func (req Request) RemainingArgCount() int {
	if req.input == nil {
		return 0
	}
	return req.input.Args.Len() - req.input.Argi
}

func (req Request) PopArg(name string) string {
	if req.input == nil || req.input.Argi >= req.input.Args.Len() {
		errors.ContextCancelWithBadRequestf(req.Context,
			"missing argument %q", name)
		return ""
	}
	v := req.input.Args[req.input.Argi]
	req.input.Argi++
	req.input.consumed = append(req.input.consumed,
		consumedArg{name: name, value: v})
	return v
}

func (req Request) PopArgs() []string {
	if req.input == nil {
		return nil
	}
	rest := slices.Clone(req.input.Args[req.input.Argi:])
	req.input.Argi = req.input.Args.Len()
	return rest
}

func (req Request) PeekArgs() []string {
	if req.input == nil {
		return nil
	}
	return slices.Clone(req.input.Args[req.input.Argi:])
}

func (req Request) LastArg() (arg string, ok bool) {
	if req.RemainingArgCount() > 0 {
		ok = true
		arg = req.PopArgs()[req.RemainingArgCount()-1]
	}
	return arg, ok
}

func (req Request) Must(fn func(interfaces.ActiveContext) error) {
	if err := fn(req.Context); err != nil {
		errors.ContextCancelWith(req.Context, err)
	}
}
```

**Note:** `Request.Utility` references `Utility`, which doesn't exist
yet — Task 8 adds it. Add a forward stub in request.go for now:

```go
// At bottom of request.go, will be removed in Task 8:
type Utility struct {
	name string
}

func (u Utility) GetName() string { return u.name }
```

**Step 7: Run all tests**

```bash
go test ./internal/command/ -v
```

Expected: PASS (one test skipped per the upstream-bug note).

**Step 8: Commit**

```bash
git add internal/command/
git commit -S -m "feat(command): add Request and CommandLineInput

Ports go/internal/delta/command/{request,command_line_input}.go from
amarbel-llc/dodder HEAD with import substitutions to dewey. Includes a
forward stub for Utility (replaced in the next commit).

Skips one LastCompleteArg test pending an upstream-bug filing.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 8: Port `command/utility.go`

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/utility.go`
- Create: `~/eng/repos/cutting-garden/internal/command/utility_run.go`
- Create: `~/eng/repos/cutting-garden/internal/command/utility_test.go`
- Modify: `~/eng/repos/cutting-garden/internal/command/request.go`
  (remove the `Utility` stub)

**Step 1: Write the failing test**

`internal/command/utility_test.go`:

```go
package command

import "testing"

type registeredCmd struct{ name string }

func (registeredCmd) Run(req Request) {}

func TestUtility_AddAndGet(t *testing.T) {
	u := MakeUtility("test", nil)
	u.AddCmd("foo", registeredCmd{name: "foo"})
	got, ok := u.GetCmd("foo")
	if !ok {
		t.Fatal("GetCmd(foo) not found")
	}
	if got.(registeredCmd).name != "foo" {
		t.Errorf("registered cmd identity not preserved")
	}
}

func TestUtility_DuplicateAddPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("duplicate AddCmd did not panic")
		}
	}()
	u := MakeUtility("test", nil)
	u.AddCmd("dup", registeredCmd{})
	u.AddCmd("dup", registeredCmd{})
}

func TestUtility_AllCmdsIterates(t *testing.T) {
	u := MakeUtility("test", nil)
	u.AddCmd("a", registeredCmd{})
	u.AddCmd("b", registeredCmd{})
	count := 0
	for range u.AllCmds() {
		count++
	}
	if count != 2 {
		t.Errorf("AllCmds count = %d, want 2", count)
	}
}

func TestUtility_GetName(t *testing.T) {
	u := MakeUtility("cutting-garden", nil)
	if got := u.GetName(); got != "cutting-garden" {
		t.Errorf("GetName = %q, want cutting-garden", got)
	}
}

func TestUtility_LenCmds(t *testing.T) {
	u := MakeUtility("test", nil)
	if got := u.LenCmds(); got != 0 {
		t.Errorf("LenCmds empty = %d, want 0", got)
	}
	u.AddCmd("a", registeredCmd{})
	if got := u.LenCmds(); got != 1 {
		t.Errorf("LenCmds after Add = %d, want 1", got)
	}
}

func TestUtility_MergeWithPrefix(t *testing.T) {
	parent := MakeUtility("parent", nil)
	child := MakeUtility("child", nil)
	child.AddCmd("op", registeredCmd{})
	parent = parent.MergeUtilityWithPrefix(child, "child")
	if _, ok := parent.GetCmd("child-op"); !ok {
		t.Error("MergeUtilityWithPrefix did not prefix the cmd name")
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run TestUtility -v
```

Expected: FAIL with "undefined: MakeUtility".

**Step 3: Write Utility**

Remove the `Utility` stub from `request.go` first.

`internal/command/utility.go` — port from dodder HEAD; substitute
imports per the table; **remove `Run`/`MakeCmdAndFlagSet`/`MakeRequest`/
`PrintUsage` for this commit** (they land in Task 9 with their own
test surface):

```go
package command

import (
	"fmt"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/foxtrot/config_cli"
)

type Config interface {
	interfaces.CommandComponentWriter
	GetConfigCLI() config_cli.Config
}

type Utility struct {
	name   string
	config Config
	cmds   map[string]Cmd
}

func MakeUtility(name string, defaultConfig Config) Utility {
	return Utility{
		name:   name,
		config: defaultConfig,
		cmds:   make(map[string]Cmd),
	}
}

func (utility Utility) GetName() string {
	return utility.name
}

func (utility Utility) GetConfig() config_cli.Config {
	if utility.config == nil {
		return config_cli.Default()
	}
	return utility.config.GetConfigCLI()
}

func (utility Utility) GetConfigAny() any {
	return utility.config
}

func (utility Utility) GetCmd(name string) (Cmd, bool) {
	cmd, ok := utility.cmds[name]
	return cmd, ok
}

func (utility Utility) LenCmds() int {
	return len(utility.cmds)
}

func (utility Utility) AllCmds() interfaces.Seq2[string, Cmd] {
	return func(yield func(string, Cmd) bool) {
		for name, cmd := range utility.cmds {
			if !yield(name, cmd) {
				return
			}
		}
	}
}

func (utility Utility) AddCmd(name string, cmd Cmd) {
	if _, ok := utility.cmds[name]; ok {
		panic("subcommand added more than once: " + name)
	}
	utility.cmds[name] = cmd
}

func (utility Utility) MergeUtilityWithPrefix(
	otherUtility Utility,
	prefix string,
) Utility {
	for name, subcommand := range otherUtility.AllCmds() {
		if prefix != "" {
			name = fmt.Sprintf("%s-%s", prefix, name)
		}
		utility.AddCmd(name, subcommand)
	}
	return utility
}

func (utility Utility) MergeUtility(otherUtility Utility) Utility {
	return utility.MergeUtilityWithPrefix(otherUtility, "")
}
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS for all tests.

**Step 5: Commit**

```bash
git add internal/command/
git commit -S -m "feat(command): add Utility with command registry

Ports the registration half of dodder's go/internal/delta/command/
utility.go: MakeUtility, AddCmd, GetCmd, AllCmds, MergeUtility, etc.
Run dispatch lands in the next commit.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 9: Add `Utility.Run` dispatch and `utility_run.go`

**Promotion criteria:** N/A.

**Files:**
- Modify: `~/eng/repos/cutting-garden/internal/command/utility.go`
- Create: `~/eng/repos/cutting-garden/internal/command/utility_run.go`
- Create: `~/eng/repos/cutting-garden/internal/command/utility_run_test.go`

**Step 1: Write the failing test**

`internal/command/utility_run_test.go`:

```go
package command

import (
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
)

type capturingCmd struct {
	receivedArgs []string
}

func (c *capturingCmd) Run(req Request) {
	c.receivedArgs = req.PopArgs()
}

func TestUtility_Run_DispatchesToRegisteredCmd(t *testing.T) {
	u := MakeUtility("test", nil)
	c := &capturingCmd{}
	u.AddCmd("greet", c)
	u.Run([]string{"test", "greet", "alice", "bob"})
	if len(c.receivedArgs) != 2 || c.receivedArgs[0] != "alice" {
		t.Errorf("dispatch did not deliver args: got %v", c.receivedArgs)
	}
}

func TestUtility_Run_NoArgs_PrintsUsageAndCancels(t *testing.T) {
	// Verify no panic; usage goes to stderr (not asserted here).
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Run with no args panicked: %v", r)
		}
	}()
	u := MakeUtility("test", nil)
	u.Run([]string{"test"})
}

func TestUtility_Run_UnknownSubcommand_Cancels(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Run with unknown subcommand panicked: %v", r)
		}
	}()
	u := MakeUtility("test", nil)
	u.Run([]string{"test", "does-not-exist"})
}

func TestExtendNameIfNecessary(t *testing.T) {
	// extendNameIfNecessary should be defensive about empty input.
	got := extendNameIfNecessary("foo")
	if got == "" {
		t.Error("extendNameIfNecessary returned empty string")
	}
	_ = interfaces.ActiveContext(nil) // ensure import is referenced
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run 'TestUtility_Run|TestExtendName' -v
```

Expected: FAIL with "Utility has no field or method Run".

**Step 3: Write the implementation**

Add to `internal/command/utility.go`:

```go
func (utility Utility) PrintUsage(ctx interfaces.ActiveContext, err error) {
	if err != nil {
		defer errors.ContextCancelWith(ctx, err)
	}
	// Phase 1: minimal usage output. Tighten in Phase 2.
	fmt.Fprintf(os.Stderr, "Usage for %s:\n", utility.name)
	for name := range utility.AllCmds() {
		fmt.Fprintln(os.Stderr, "  "+name)
	}
}

func (utility Utility) Run(args []string) {
	utilityNameWithExtension := extendNameIfNecessary(utility.GetName())
	ctx := errors.MakeContextDefault()
	ctx.SetCancelOnSignals(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	if err := ctx.Run(func(ctx interfaces.ActiveContext) {
		if len(args) <= 1 {
			utility.PrintUsage(ctx,
				errors.BadRequestf("No subcommand provided."))
			return
		}

		cmd, flagSet, ok := utility.MakeCmdAndFlagSet(ctx, args)
		if !ok {
			return
		}

		req, ok := utility.MakeRequest(ctx, cmd, flagSet)
		if !ok {
			return
		}

		cmd.Run(req)
	}); err != nil {
		os.Exit(handleMainErrors(ctx, utilityNameWithExtension, err))
	}
}

func (utility Utility) MakeCmdAndFlagSet(
	ctx interfaces.ActiveContext,
	args []string,
) (cmd Cmd, flagSet *flags.FlagSet, ok bool) {
	name := args[1]

	if cmd, ok = utility.GetCmd(name); !ok {
		utility.PrintUsage(ctx, errors.BadRequestf("No subcommand %q", name))
		return
	}

	flagSet = flags.NewFlagSet(name, flags.ContinueOnError)

	if w, ok := cmd.(interfaces.CommandComponentWriter); ok {
		w.SetFlagDefinitions(flagSet)
	}

	rest := args[2:]

	if utility.config != nil {
		utility.config.SetFlagDefinitions(flagSet)
	}

	if err := flagSet.Parse(rest); err != nil {
		if errors.Is(err, flags.ErrHelp) {
			ok = false
			return
		}
		errors.ContextCancelWith(ctx, err)
	}

	return cmd, flagSet, true
}

func (utility Utility) MakeRequest(
	ctx interfaces.ActiveContext,
	cmd Cmd,
	flagSet *flags.FlagSet,
) (request Request, ok bool) {
	parsed := flagSet.Args()
	input := CommandLineInput{
		FlagsOrArgs: collections_slice.String(parsed),
		Args:        collections_slice.String(parsed),
	}
	if input.Args.Len() > 0 && input.Args.First() == "--" {
		input.ContainsDoubleHyphen = true
		input.Args.ShiftInPlace(1)
	}
	return Request{
		Utility: utility,
		Context: ctx,
		FlagSet: flagSet,
		input:   &input,
	}, true
}
```

Add the imports `os`, `syscall`, `errors`, `flags`,
`collections_slice` to utility.go.

`internal/command/utility_run.go`:

```go
package command

import (
	"fmt"
	"runtime"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
)

func extendNameIfNecessary(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func handleMainErrors(
	ctx interfaces.ActiveContext,
	utilityName string,
	err error,
) int {
	if err == nil {
		return 0
	}
	fmt.Fprintf(ctx.GetErr(), "%s: %s\n", utilityName, err)
	if errors.IsBadRequest(err) {
		return 64 // EX_USAGE
	}
	return 1
}
```

**Note:** if `errors.IsBadRequest` doesn't exist on dewey, drop the
exit-code distinction and just `return 1` for any error. Verify with
`hamster.doc code.linenisgreat.com/purse-first/libs/dewey/bravo/errors`.

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/command/utility.go internal/command/utility_run.go internal/command/utility_run_test.go
git commit -S -m "feat(command): add Utility.Run dispatch

Ports Run, MakeCmdAndFlagSet, MakeRequest, PrintUsage from dodder HEAD
with the dewey errors-context substitution.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 10: Port `command/completion.go`

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/completion.go`
- Create: `~/eng/repos/cutting-garden/internal/command/completion_test.go`

**Step 1: Write the failing test**

`internal/command/completion_test.go`:

```go
package command

import "testing"

func TestCompleter_Implementable(t *testing.T) {
	called := false
	c := completerFunc(func(req Request, env any, cli CommandLineInput) {
		called = true
	})
	c.Complete(Request{}, nil, CommandLineInput{})
	if !called {
		t.Error("Completer.Complete was not invoked")
	}
}

type completerFunc func(Request, any, CommandLineInput)

func (f completerFunc) Complete(req Request, env any, cli CommandLineInput) {
	f(req, env, cli)
}

var _ Completer = completerFunc(nil)

func TestFlagValueCompleter_NilFlagValue_StringEmpty(t *testing.T) {
	fvc := FlagValueCompleter{}
	if got := fvc.String(); got != "" {
		t.Errorf("nil FlagValue String() = %q, want empty", got)
	}
}

func TestCompletion_Fields(t *testing.T) {
	c := Completion{Value: "v", Description: "d"}
	if c.Value != "v" || c.Description != "d" {
		t.Errorf("Completion roundtrip failed: %+v", c)
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run 'TestCompleter|TestFlagValueCompleter|TestCompletion' -v
```

Expected: FAIL with "undefined: Completer".

**Step 3: Write implementation**

`internal/command/completion.go` (note the `any` substitution per the
design doc):

```go
package command

import "code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"

type SupportsCompletion interface {
	SupportsCompletion()
}

type Completion struct {
	Value, Description string
}

// Completer is implemented by commands and flag values that provide
// shell completions. The env parameter is application-specific —
// cutting-garden commands type-assert it to env_local.Env. Kept as
// `any` for framework portability.
type Completer interface {
	Complete(Request, any, CommandLineInput)
}

type FuncCompleter func(Request, any, CommandLineInput)

type FlagValueCompleter struct {
	interfaces.FlagValue
	FuncCompleter
}

func (completer FlagValueCompleter) String() string {
	if completer.FlagValue == nil {
		return ""
	}
	return completer.FlagValue.String()
}

func (completer FlagValueCompleter) Complete(
	req Request,
	env any,
	commandLine CommandLineInput,
) {
	completer.FuncCompleter(req, env, commandLine)
}
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/command/completion.go internal/command/completion_test.go
git commit -S -m "feat(command): add Completer interface and FlagValueCompleter

Ports go/internal/delta/command/completion.go from dodder HEAD with
the second arg typed as 'any' instead of env_local.Env (design doc:
docs/plans/2026-05-10-extract-cutting-garden-design.md).

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 11: Add the visible `complete` subcommand with repaired positional dispatch

**Promotion criteria:** Closes the positional-completion gap that
issue [madder#161](https://github.com/amarbel-llc/madder/issues/161)
captures, *for the cutting-garden side only*. The madder side keeps
the gap.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/complete.go`
- Create: `~/eng/repos/cutting-garden/internal/command/complete_test.go`

This is the centerpiece of Phase 1 — the bug fix that motivated the
whole brainstorm.

**Step 1: Write the failing test**

`internal/command/complete_test.go`:

```go
package command

import (
	"bytes"
	"strings"
	"testing"
)

type completerCmd struct {
	emit []Completion
}

func (c completerCmd) Run(req Request) {}

func (c completerCmd) Complete(req Request, env any, cli CommandLineInput) {
	out := req.Context.GetOut()
	for _, c := range c.emit {
		fmt.Fprintf(out, "%s\t%s\n", c.Value, c.Description)
	}
}

type plainCmd struct{}

func (plainCmd) Run(req Request) {}

func TestComplete_BareInvocation_ListsSubcommands(t *testing.T) {
	u := MakeUtility("test", nil)
	u.AddCmd("alpha", plainCmd{})
	u.AddCmd("beta", plainCmd{})

	var buf bytes.Buffer
	out := captureComplete(t, &u, &buf, []string{"complete"})

	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Errorf("bare complete did not list subcommands: %q", out)
	}
}

func TestComplete_PositionalDispatch_CallsCompleter(t *testing.T) {
	u := MakeUtility("test", nil)
	u.AddCmd("sub", completerCmd{
		emit: []Completion{{Value: "first", Description: "v1"}},
	})

	var buf bytes.Buffer
	out := captureComplete(t, &u, &buf, []string{"complete", "sub", ""})

	if !strings.Contains(out, "first") {
		t.Errorf("positional dispatch did not call Completer: %q", out)
	}
}

func TestComplete_PositionalDispatch_NonCompleter_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("non-Completer subcommand panicked: %v", r)
		}
	}()
	u := MakeUtility("test", nil)
	u.AddCmd("plain", plainCmd{})
	var buf bytes.Buffer
	captureComplete(t, &u, &buf, []string{"complete", "plain", ""})
}

// captureComplete is a test helper that wires the complete subcommand
// against a fixture utility. Implementation in the same test file.
func captureComplete(t *testing.T, u *Utility, buf *bytes.Buffer, args []string) string {
	// TODO: wire the new RegisterComplete(util) call once it exists.
	// For now, fail the test loudly so we know to revisit.
	t.Helper()
	RegisterComplete(u)
	// Inject buf as the utility's stdout via a test-only hook (added in
	// implementation step).
	withTestStdout(buf, func() {
		u.Run(append([]string{"test"}, args...))
	})
	return buf.String()
}

func withTestStdout(buf *bytes.Buffer, fn func()) {
	old := testStdoutHook
	testStdoutHook = buf
	defer func() { testStdoutHook = old }()
	fn()
}
```

**Note:** the test relies on a `RegisterComplete(u)` helper and a
`testStdoutHook` indirection to keep the production path clean. Both
are added in Step 3.

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run TestComplete -v
```

Expected: FAIL with "undefined: RegisterComplete".

**Step 3: Write the `complete` subcommand**

`internal/command/complete.go`:

```go
package command

import (
	"fmt"
	"io"
	"os"
	"strings"

	"code.linenisgreat.com/purse-first/libs/dewey/0/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/bravo/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/charlie/flags"
)

// testStdoutHook lets tests intercept the complete subcommand's
// output. Production code falls back to os.Stdout when nil.
var testStdoutHook io.Writer

func completeOut() io.Writer {
	if testStdoutHook != nil {
		return testStdoutHook
	}
	return os.Stdout
}

// RegisterComplete attaches the visible `complete` subcommand to a
// utility. Call this once during utility construction. Idempotent: a
// second call is a no-op (panics from AddCmd otherwise).
func RegisterComplete(u *Utility) {
	if _, exists := u.GetCmd("complete"); exists {
		return
	}
	u.AddCmd("complete", &completeCmd{util: u})
}

type completeCmd struct {
	util       *Utility
	bashStyle  bool
	inProgress string
}

func (c *completeCmd) GetDescription() Description {
	return Description{Short: "complete a command-line"}
}

func (c *completeCmd) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(&c.bashStyle, "bash-style", false,
		"emit bash-style completions")
	flagSet.StringVar(&c.inProgress, "in-progress", "",
		"the partial token currently being completed")
}

func (c *completeCmd) Run(req Request) {
	commandLine := CommandLineInput{
		FlagsOrArgs: append(append(make([]string, 0), req.PeekArgs()...)),
		InProgress:  c.inProgress,
	}

	lastArg, hasLastArg := commandLine.LastArg()
	if !hasLastArg {
		c.completeSubcommands()
		return
	}

	name := req.PopArg("name")
	subcmd, found := c.util.GetCmd(name)
	if !found {
		c.completeSubcommands()
		return
	}

	flagSet := flags.NewFlagSet(name, flags.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	if w, ok := subcmd.(interfaces.CommandComponentWriter); ok {
		w.SetFlagDefinitions(flagSet)
	}

	containsDoubleHyphen := false
	for _, a := range commandLine.FlagsOrArgs {
		if a == "--" {
			containsDoubleHyphen = true
			break
		}
	}

	if !containsDoubleHyphen &&
		c.completeSubcommandFlags(req, subcmd, flagSet, commandLine, lastArg) {
		return
	}

	c.completeSubcommandArgs(req, subcmd, commandLine)
}

func (c *completeCmd) completeSubcommands() {
	out := completeOut()
	for name, subcmd := range c.util.AllCmds() {
		if d, ok := subcmd.(CommandWithDescription); ok {
			fmt.Fprintf(out, "%s\t%s\n", name, d.GetDescription().Short)
		} else {
			fmt.Fprintln(out, name)
		}
	}
}

// completeSubcommandArgs is the *repaired* positional-completion
// dispatch. The madder-side legacy implementation gutted this
// function (TODO #48); we restore it here.
func (c *completeCmd) completeSubcommandArgs(
	req Request,
	subcmd Cmd,
	commandLine CommandLineInput,
) {
	if completer, ok := subcmd.(Completer); ok {
		// Phase 1 has no env_local plumbing in the framework; pass nil
		// for the env arg. Cutting-garden commands that need env type-
		// assert at call site (per design doc).
		completer.Complete(req, nil, commandLine)
	}
}

func (c *completeCmd) completeSubcommandFlags(
	req Request,
	subcmd Cmd,
	flagSet *flags.FlagSet,
	commandLine CommandLineInput,
	lastArg string,
) (handled bool) {
	if strings.HasPrefix(lastArg, "-") && commandLine.InProgress != "" {
		handled = true
	} else if commandLine.InProgress != "" && len(commandLine.FlagsOrArgs) > 1 {
		lastArg = commandLine.FlagsOrArgs[len(commandLine.FlagsOrArgs)-2]
		commandLine.InProgress = ""
		handled = strings.HasPrefix(lastArg, "-")
	}

	out := completeOut()
	if commandLine.InProgress != "" {
		flagSet.VisitAll(func(flag *flags.Flag) {
			fmt.Fprintf(out, "-%s\t%s\n", flag.Name, flag.Usage)
		})
		return handled
	}

	if err := flagSet.Parse([]string{lastArg}); err != nil {
		c.completeSubcommandFlagOnParseError(req, subcmd, flagSet, commandLine, err)
	} else {
		flagSet.VisitAll(func(flag *flags.Flag) {
			fmt.Fprintf(out, "-%s\t%s\n", flag.Name, flag.Usage)
		})
	}
	return handled
}

func (c *completeCmd) completeSubcommandFlagOnParseError(
	req Request,
	subcmd Cmd,
	flagSet *flags.FlagSet,
	commandLine CommandLineInput,
	err error,
) {
	after, found := strings.CutPrefix(err.Error(), "flag needs an argument: -")
	if !found {
		errors.ContextCancelWith(req.Context, err)
		return
	}

	flag := flagSet.Lookup(after)
	if flag == nil {
		errors.ContextCancelWith(req.Context,
			errors.New(fmt.Sprintf("flag %q not found", after)))
		return
	}

	switch fv := flag.Value.(type) {
	case Completer:
		fv.Complete(req, nil, commandLine)
	default:
		errors.ContextCancelWith(req.Context,
			errors.New(fmt.Sprintf("no completion for flag %q", after)))
	}
}
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS for all complete tests.

**Step 5: Commit**

```bash
git add internal/command/complete.go internal/command/complete_test.go
git commit -S -m "feat(command): add complete subcommand with repaired positional dispatch

Adds the visible \`complete\` subcommand. RegisterComplete(util) wires
it onto a Utility. Positional-arg dispatch now invokes
cmd.(Completer).Complete(...) on the registered instance — the bug
that motivated this whole effort (madder#161).

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 12: Add the manpage shim — types and opt-in interfaces

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/manpage.go`
- Create: `~/eng/repos/cutting-garden/internal/command/manpage_test.go`

**Step 1: Write the failing test**

`internal/command/manpage_test.go`:

```go
package command

import "testing"

type manpageFixture struct{}

func (manpageFixture) Run(req Request) {}

func (manpageFixture) GetEnvVars() []EnvVar {
	return []EnvVar{{Name: "FOO", Description: "foo var"}}
}

func (manpageFixture) GetFiles() []FilePath {
	return []FilePath{{Path: "$HOME/.foo", Description: "config"}}
}

func (manpageFixture) GetSeeAlso() []string {
	return []string{"bar(1)"}
}

func (manpageFixture) GetExamples() []Example {
	return []Example{{Description: "do thing", Command: "foo bar"}}
}

func TestEnvVar_Fields(t *testing.T) {
	e := EnvVar{Name: "X", Description: "y", Default: "z"}
	if e.Name != "X" || e.Description != "y" || e.Default != "z" {
		t.Errorf("EnvVar roundtrip failed: %+v", e)
	}
}

func TestFilePath_Fields(t *testing.T) {
	f := FilePath{Path: "/etc/foo", Description: "config"}
	if f.Path == "" || f.Description == "" {
		t.Error("FilePath roundtrip failed")
	}
}

func TestExample_Fields(t *testing.T) {
	e := Example{Description: "d", Command: "c", Output: "o"}
	if e.Description != "d" || e.Command != "c" || e.Output != "o" {
		t.Errorf("Example roundtrip failed: %+v", e)
	}
}

func TestCommandWithEnvVars_Implementable(t *testing.T) {
	var c CommandWithEnvVars = manpageFixture{}
	if got := len(c.GetEnvVars()); got != 1 {
		t.Errorf("GetEnvVars len = %d, want 1", got)
	}
}

func TestCommandWithFiles_Implementable(t *testing.T) {
	var _ CommandWithFiles = manpageFixture{}
}

func TestCommandWithSeeAlso_Implementable(t *testing.T) {
	var _ CommandWithSeeAlso = manpageFixture{}
}

func TestCommandWithExamples_Implementable(t *testing.T) {
	var _ CommandWithExamples = manpageFixture{}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run 'TestEnvVar|TestFilePath|TestExample|TestCommandWith' -v
```

Expected: FAIL with "undefined: EnvVar".

**Step 3: Write implementation**

`internal/command/manpage.go`:

```go
package command

import "io/fs"

type EnvVar struct {
	Name        string
	Description string
	Default     string
}

type FilePath struct {
	Path        string
	Description string
}

type ManpageFile struct {
	Source  fs.FS
	Path    string
	Section int
	Name    string
}

type Example struct {
	Description string
	Command     string
	Output      string
}

type CommandWithEnvVars interface {
	GetEnvVars() []EnvVar
}

type CommandWithFiles interface {
	GetFiles() []FilePath
}

type CommandWithSeeAlso interface {
	GetSeeAlso() []string
}

type CommandWithExamples interface {
	GetExamples() []Example
}

type CommandWithManpageFiles interface {
	GetManpageFiles() []ManpageFile
}
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/command/manpage.go internal/command/manpage_test.go
git commit -S -m "feat(command): add manpage shim types and opt-in interfaces

Provides EnvVar, FilePath, ManpageFile, Example types and
CommandWith{EnvVars,Files,SeeAlso,Examples,ManpageFiles} interfaces
that command instances implement opt-in to surface metadata to manpage
generation.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 13: Add manpage generator

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/generate_manpages.go`
- Create: `~/eng/repos/cutting-garden/internal/command/generate_manpages_test.go`

**Step 1: Write the failing test (golden test)**

`internal/command/generate_manpages_test.go`:

```go
package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type goldenCmd struct{}

func (goldenCmd) Run(req Request) {}

func (goldenCmd) GetDescription() Description {
	return Description{
		Short: "do the thing",
		Long:  "Does the thing in a configurable, well-described way.",
	}
}

func (goldenCmd) GetEnvVars() []EnvVar {
	return []EnvVar{
		{Name: "DEMO_VERBOSE", Description: "enable verbose mode", Default: "0"},
	}
}

func TestGenerateManpages_BasicCommand(t *testing.T) {
	dir := t.TempDir()
	u := MakeUtility("demo", nil)
	u.AddCmd("thing", goldenCmd{})

	if err := u.GenerateManpages(dir); err != nil {
		t.Fatalf("GenerateManpages: %v", err)
	}

	wantPath := filepath.Join(dir, "share", "man", "man1", "demo-thing.1")
	body, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("missing manpage at %s: %v", wantPath, err)
	}
	got := string(body)

	for _, want := range []string{
		"demo-thing",
		"do the thing",
		"DEMO_VERBOSE",
		"enable verbose mode",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manpage missing %q\n--- got ---\n%s", want, got)
		}
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run TestGenerateManpages -v
```

Expected: FAIL with "u.GenerateManpages undefined".

**Step 3: Write implementation**

`internal/command/generate_manpages.go`:

```go
package command

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (u *Utility) GenerateManpages(outDir string) error {
	manDir := filepath.Join(outDir, "share", "man", "man1")
	if err := os.MkdirAll(manDir, 0o755); err != nil {
		return err
	}
	for name, cmd := range u.AllCmds() {
		body, err := u.renderManpage(name, cmd)
		if err != nil {
			return err
		}
		path := filepath.Join(manDir, fmt.Sprintf("%s-%s.1", u.GetName(), name))
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (u *Utility) renderManpage(name string, cmd Cmd) (string, error) {
	var b strings.Builder
	page := fmt.Sprintf("%s-%s", u.GetName(), name)
	fmt.Fprintf(&b, ".TH %s 1\n", strings.ToUpper(page))
	fmt.Fprintf(&b, ".SH NAME\n%s", page)

	if d, ok := cmd.(CommandWithDescription); ok {
		desc := d.GetDescription()
		if desc.Short != "" {
			fmt.Fprintf(&b, " - %s", desc.Short)
		}
		fmt.Fprintln(&b)
		if desc.Long != "" {
			fmt.Fprintf(&b, ".SH DESCRIPTION\n%s\n", desc.Long)
		}
	} else {
		fmt.Fprintln(&b)
	}

	if a, ok := cmd.(CommandWithArgs); ok {
		groups := a.GetArgs()
		if len(groups) > 0 {
			fmt.Fprintln(&b, ".SH ARGUMENTS")
			for _, g := range groups {
				for _, arg := range g.Args {
					fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", arg.Name, arg.Description)
				}
			}
		}
	}

	if e, ok := cmd.(CommandWithEnvVars); ok {
		envs := e.GetEnvVars()
		if len(envs) > 0 {
			fmt.Fprintln(&b, ".SH ENVIRONMENT")
			for _, env := range envs {
				fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", env.Name, env.Description)
				if env.Default != "" {
					fmt.Fprintf(&b, "Default: %s\n", env.Default)
				}
			}
		}
	}

	if f, ok := cmd.(CommandWithFiles); ok {
		files := f.GetFiles()
		if len(files) > 0 {
			fmt.Fprintln(&b, ".SH FILES")
			for _, file := range files {
				fmt.Fprintf(&b, ".TP\n.I %s\n%s\n", file.Path, file.Description)
			}
		}
	}

	if e, ok := cmd.(CommandWithExamples); ok {
		examples := e.GetExamples()
		if len(examples) > 0 {
			fmt.Fprintln(&b, ".SH EXAMPLES")
			for _, ex := range examples {
				fmt.Fprintf(&b, ".TP\n%s\n.B %s\n", ex.Description, ex.Command)
				if ex.Output != "" {
					fmt.Fprintf(&b, "Output: %s\n", ex.Output)
				}
			}
		}
	}

	if s, ok := cmd.(CommandWithSeeAlso); ok {
		others := s.GetSeeAlso()
		if len(others) > 0 {
			fmt.Fprintln(&b, ".SH SEE ALSO")
			fmt.Fprintln(&b, strings.Join(others, ", "))
		}
	}

	return b.String(), nil
}
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/command/generate_manpages.go internal/command/generate_manpages_test.go
git commit -S -m "feat(command): add manpage generator

Reads CommandWith{Description,Args,EnvVars,Files,Examples,SeeAlso}
opt-in interfaces off each registered Cmd instance and emits a basic
man(1) page per subcommand to share/man/man1/<utility>-<cmd>.1.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 14: Add stub-style completion generator

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/internal/command/generate_completions.go`
- Create: `~/eng/repos/cutting-garden/internal/command/generate_completions_test.go`

**Step 1: Write the failing test**

`internal/command/generate_completions_test.go`:

```go
package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateCompletions_BashStub(t *testing.T) {
	dir := t.TempDir()
	u := MakeUtility("demo", nil)
	if err := u.GenerateCompletions(dir); err != nil {
		t.Fatalf("GenerateCompletions: %v", err)
	}
	path := filepath.Join(dir, "share", "bash-completion", "completions", "demo")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing bash stub: %v", err)
	}
	got := string(body)
	for _, want := range []string{
		"_demo()",
		"demo complete --bash-style",
		"complete -F _demo demo",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bash stub missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestGenerateCompletions_FishStub(t *testing.T) {
	dir := t.TempDir()
	u := MakeUtility("demo", nil)
	_ = u.GenerateCompletions(dir)
	path := filepath.Join(dir, "share", "fish", "vendor_completions.d", "demo.fish")
	body, _ := os.ReadFile(path)
	got := string(body)
	if !strings.Contains(got, "demo complete --bash-style") {
		t.Errorf("fish stub does not delegate to complete subcommand: %q", got)
	}
}

func TestGenerateCompletions_ZshStub(t *testing.T) {
	dir := t.TempDir()
	u := MakeUtility("demo", nil)
	_ = u.GenerateCompletions(dir)
	path := filepath.Join(dir, "share", "zsh", "site-functions", "_demo")
	body, _ := os.ReadFile(path)
	got := string(body)
	if !strings.Contains(got, "demo complete") {
		t.Errorf("zsh stub does not delegate to complete subcommand: %q", got)
	}
}
```

**Step 2: Run to verify failure**

```bash
go test ./internal/command/ -run TestGenerateCompletions -v
```

Expected: FAIL with "u.GenerateCompletions undefined".

**Step 3: Write implementation**

`internal/command/generate_completions.go`:

```go
package command

import (
	"fmt"
	"os"
	"path/filepath"
)

func (u *Utility) GenerateCompletions(outDir string) error {
	if err := u.generateBashStub(outDir); err != nil {
		return err
	}
	if err := u.generateFishStub(outDir); err != nil {
		return err
	}
	return u.generateZshStub(outDir)
}

func (u *Utility) generateBashStub(outDir string) error {
	dir := filepath.Join(outDir, "share", "bash-completion", "completions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`# bash completion for %[1]s
_%[1]s() {
    local cur words cword
    _get_comp_words_by_ref -n =: cur words cword
    local in_progress="${cur}"
    COMPREPLY=( $(compgen -W "$(%[1]s complete --bash-style --in-progress="${in_progress}" -- "${words[@]:1}")" -- "${cur}") )
}
complete -F _%[1]s %[1]s
`, u.GetName())
	return os.WriteFile(filepath.Join(dir, u.GetName()), []byte(body), 0o644)
}

func (u *Utility) generateFishStub(outDir string) error {
	dir := filepath.Join(outDir, "share", "fish", "vendor_completions.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`# fish completion for %[1]s
function __%[1]s_complete
    set -l args (commandline -opc)
    set -l cur (commandline -ct)
    %[1]s complete --bash-style --in-progress="$cur" -- $args[2..]
end
complete -c %[1]s -f -a '(__%[1]s_complete)'
`, u.GetName())
	return os.WriteFile(filepath.Join(dir, u.GetName()+".fish"), []byte(body), 0o644)
}

func (u *Utility) generateZshStub(outDir string) error {
	dir := filepath.Join(outDir, "share", "zsh", "site-functions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`#compdef %[1]s
_%[1]s() {
    local -a candidates
    candidates=( ${(f)"$(%[1]s complete --bash-style --in-progress="${words[CURRENT]}" -- "${words[@]:1}")"} )
    _describe 'completions' candidates
}
_%[1]s "$@"
`, u.GetName())
	return os.WriteFile(filepath.Join(dir, "_"+u.GetName()), []byte(body), 0o644)
}
```

**Step 4: Run to verify pass**

```bash
go test ./internal/command/ -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/command/generate_completions.go internal/command/generate_completions_test.go
git commit -S -m "feat(command): add stub-style completion generator

Emits ~30-line bash/fish/zsh stubs per binary that delegate to
'<binary> complete --bash-style …' for all dynamic completion. The
generated scripts contain no per-command knowledge — that lives in
the binary itself. Replaces ~700 lines of futility-style per-command
generators.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 15: Add `cmd/cutting-garden/main.go`

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/cmd/cutting-garden/main.go`

**Step 1: Write the binary entrypoint**

`cmd/cutting-garden/main.go`:

```go
package main

import (
	"os"

	"github.com/amarbel-llc/cutting-garden/internal/command"
)

func main() {
	utility := command.MakeUtility("cutting-garden", nil)
	command.RegisterComplete(&utility)
	utility.Run(os.Args)
}
```

**Step 2: Verify it builds**

```bash
cd ~/eng/repos/cutting-garden
go build ./cmd/cutting-garden
ls -l cutting-garden
```

Expected: a `cutting-garden` binary in the repo root.

**Step 3: Run it without args**

```bash
./cutting-garden
echo "exit=$?"
```

Expected: usage output to stderr, exit code 64 (or 1 if dewey doesn't
expose `IsBadRequest`). Lists `complete` as the only subcommand.

**Step 4: Run the complete subcommand bare**

```bash
./cutting-garden complete
```

Expected: a single line `complete\tcomplete a command-line` printed to
stdout.

**Step 5: Clean up the test binary**

```bash
rm cutting-garden
```

**Step 6: Commit**

```bash
git add cmd/cutting-garden/main.go
git commit -S -m "feat: add cutting-garden binary entrypoint

Boots the framework with no commands registered other than the
auto-registered \`complete\` helper. Sufficient to demonstrate the
framework end-to-end before Phase 2 ports capture/restore/diff.

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 16: Add `flake.nix`

**Promotion criteria:** N/A.

**Files:**
- Create: `~/eng/repos/cutting-garden/flake.nix`
- Modify: `~/eng/repos/cutting-garden/.github/workflows/ci.yml` (add nix
  build job)

**Step 1: Write a minimal flake.nix**

Use madder's `flake.nix` as a template; `chix.flake-show` against madder
will reveal the relevant attributes. Keep cutting-garden's flake to
just `packages.default = cutting-garden`.

`flake.nix` (sketch — adapt from madder's, simplifying):

```nix
{
  description = "cutting-garden — filesystem capture/restore CLI atop madder";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        packages.default = pkgs.buildGoModule {
          pname = "cutting-garden";
          version = "0.0.1";
          src = ./.;
          vendorHash = null;  # populated after first `nix build` and `go mod vendor`
          subPackages = [ "cmd/cutting-garden" ];

          postInstall = ''
            $out/bin/cutting-garden __generate-manpages $out
            $out/bin/cutting-garden __generate-completions $out
          '';

          meta = with pkgs.lib; {
            description = "Filesystem-tree capture/restore CLI atop madder";
            license = licenses.mit;
            mainProgram = "cutting-garden";
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [ go gopls ];
        };
      });
}
```

**Note:** `__generate-manpages` and `__generate-completions` aren't
hidden subcommands yet — Phase 1's framework calls them as Go programs,
not subcommands of the running binary. Add a tiny `cmd/gen/main.go`
that imports the framework and invokes `GenerateManpages`/
`GenerateCompletions`, OR replace the postInstall lines with a separate
build step that runs the gen tools. Pragmatic Phase 1 choice: skip the
postInstall manpage/completion generation for now (it's framework
dogfooding; Phase 2 reuses the same primitive). Mark this in the
flake.nix with a TODO.

Final flake.nix `postInstall`:

```nix
postInstall = ''
  # TODO: run manpage + completion generation in Phase 2 once we
  # have a generator-binary entrypoint.
'';
```

**Step 2: Run nix build**

```bash
cd ~/eng/repos/cutting-garden
nix build .#default
```

If it fails on `vendorHash = null`, it'll print the expected hash.
Update the file with that hash and re-run.

Expected: a `result/` symlink with `result/bin/cutting-garden`.

**Step 3: Verify the binary runs**

```bash
./result/bin/cutting-garden complete
```

Expected: same `complete\tcomplete a command-line` line.

**Step 4: Add nix-build job to CI**

Edit `.github/workflows/ci.yml`, append:

```yaml
  nix-build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: cachix/install-nix-action@v27
        with:
          nix_path: nixpkgs=channel:nixos-unstable
      - run: nix build .#default
```

**Step 5: Commit**

```bash
git add flake.nix .github/workflows/ci.yml
git commit -S -m "build: add nix flake and CI nix-build

Builds the cutting-garden binary via buildGoModule. Manpage and
completion generation deferred to Phase 2 (needs a generator-binary
entrypoint).

Signed-off-by: Clown <https://github.com/amarbel-llc/clown> :clown:"
```

---

## Task 17: File upstream bug for `LastCompleteArg`

**Promotion criteria:** Track the dodder-side bug we carried forward in
Task 7.

**Step 1: File issue against dodder**

```bash
cd ~/eng/repos/cutting-garden
gh issue create \
  --repo amarbel-llc/dodder \
  --title 'CommandLineInput.LastCompleteArg returns the wrong index when InProgress matches the last arg' \
  --body "$(cat <<'EOF'
\`go/internal/delta/command/command_line_input.go::LastCompleteArg\`
decrements \`argc\` when \`InProgress != \"\"\`, but then returns
\`commandLine.FlagsOrArgs.Last()\` regardless — i.e. the unmodified
last element. Should return \`FlagsOrArgs[argc-1]\` (the
last-before-in-progress element).

Carried forward verbatim into the cutting-garden port at
github.com/amarbel-llc/cutting-garden/internal/command/command_line_input.go;
filing here so both repos can be fixed together.

Captured by Clown — https://github.com/amarbel-llc/clown :clown:
EOF
)"
```

**Step 2: File matching issue against cutting-garden**

```bash
gh issue create \
  --repo amarbel-llc/cutting-garden \
  --title 'CommandLineInput.LastCompleteArg has a port-of-bug from dodder' \
  --body "Tracks the upstream bug at https://github.com/amarbel-llc/dodder/issues/<N> (replace <N> after dodder issue is filed). Resolves when both repos fix the function and the test in command_line_input_test.go is unskipped."
```

**Step 3: Cross-link the issues**

After both issues exist, edit each one's body to reference the other
(use `gh issue edit`).

**Step 4: No commit needed** — issue filing is metadata.

---

## Final verification

After all tasks:

**Step 1: Full test pass**

```bash
cd ~/eng/repos/cutting-garden
go test ./... -v
go vet ./...
```

Expected: all PASS, no vet warnings.

**Step 2: Nix build clean**

```bash
nix build .#default
./result/bin/cutting-garden complete
```

Expected: binary builds; complete subcommand prints itself.

**Step 3: CI passes on the new repo**

Push to GitHub:

```bash
git push origin master
```

Expected: GitHub Actions runs `go test`, `go vet`, and `nix build`; all
green. Confirm via `gh run list --repo amarbel-llc/cutting-garden`.

---

## Phase 2 hand-off

Once Phase 1 lands:

- The repo is ready to host the first command port (capture).
- A new design doc `docs/plans/2026-MM-DD-cutting-garden-capture.md`
  (in either madder or the new repo — TBD) covers porting capture,
  including the `command_components` decision (mixin vs direct pkgs
  consumption).
- Issue [madder#161](https://github.com/amarbel-llc/madder/issues/161)
  gets a comment noting that the new-repo cutting-garden side has the
  fix, with a link to this implementation plan; the madder-side gap
  is left open.
- Issue [madder#162](https://github.com/amarbel-llc/madder/issues/162)
  (path-cleanup in capture receipts) stays scoped to madder until
  capture is ported.
