# SFTP Test Harness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Port dodder's `test-sftp-server` into madder as `madder-test-sftp-server` with an RFC-0001-conformant handshake, then add four observer-focused SFTP bats scenarios behind a `net_cap` bats tag.

**Architecture:** A devshell-only Go binary implements the RFC-0001 handshake protocol and serves an embedded SSH/SFTP server on `127.0.0.1:0`. A new `zz-tests_bats/lib/sftp.bash` helper spawns the binary as a coprocess, parses the handshake, and exports `$SFTP_PORT` / `$SFTP_KNOWN_HOSTS` for test bodies. A new `test-bats-net-cap` justfile recipe runs the `net_cap`-tagged `sftp.bats` under sandcastle with `--allow-local-binding`; the existing `test-bats` recipe is updated to filter `!net_cap` so other tests stay under default-deny.

**Tech Stack:** Go (`crypto/ecdsa`, `crypto/ssh`, `github.com/pkg/sftp`), nix (`buildGoModule`), bats-core + bats-emo, sandcastle/batman.

**Rollback:** Purely additive. If flaky, remove `test-bats-net-cap` from the `test` composition in `justfile` (one-line revert). The `# bats file_tags=net_cap` tag makes the sftp.bats file already excluded from the default `test-bats` recipe.

**Design:** `docs/plans/2026-04-24-sftp-test-harness-design.md`. **Protocol RFC:** `docs/rfcs/0001-test-subprocess-handshake-protocol.md`. **Prior art:** `~/eng/repos/dodder/go/cmd/test-sftp-server/main.go` (reference port source, ~137 lines).

---

### Task 1: Scaffold the Go package with cookie-check-only + failing test

**Promotion criteria:** N/A (new code).

**Files:**
- Create: `go/cmd/madder-test-sftp-server/main.go`
- Create: `go/cmd/madder-test-sftp-server/main_test.go`

**Step 1: Write the failing test**

```go
//go:build test

package main

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestCookieMismatchExitsOne asserts RFC 0001's Cookie Envelope
// normative requirement: without MADDER_PLUGIN_COOKIE set, the binary
// MUST print "[<name>] magic cookie mismatch" to stderr and exit 1
// with no stdout output.
func TestCookieMismatchExitsOne(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	cmd.Env = []string{} // explicitly no MADDER_PLUGIN_COOKIE
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "[madder-test-sftp-server] magic cookie mismatch") {
		t.Errorf("stderr = %q, want [madder-test-sftp-server] magic cookie mismatch prefix", stderr.String())
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go -run TestCookieMismatchExitsOne ./cmd/madder-test-sftp-server/...`
Expected: BUILD FAIL — `main.go` doesn't exist yet.

**Step 3: Write minimal implementation**

```go
// Package main is the test-only SFTP server described in RFC 0001.
// Normally invoked by bats helpers via MADDER_PLUGIN_COOKIE; refuses
// to start without the envelope so accidental direct invocation on a
// shared machine fails loudly.
package main

import (
	"fmt"
	"os"
)

const programName = "madder-test-sftp-server"

func main() {
	if os.Getenv("MADDER_PLUGIN_COOKIE") == "" {
		fmt.Fprintf(os.Stderr, "[%s] magic cookie mismatch\n", programName)
		os.Exit(1)
	}

	// Remainder lands in later tasks. For now an empty cookie check
	// is enough to satisfy TestCookieMismatchExitsOne.
	os.Exit(0)
}
```

**Step 4: Run test to verify it passes**

Run: `just test-go -run TestCookieMismatchExitsOne ./cmd/madder-test-sftp-server/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/cmd/madder-test-sftp-server/
git commit -m "feat(madder-test-sftp-server): cookie-mismatch exit path"
```

---

### Task 2: Handshake line emission + test

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/cmd/madder-test-sftp-server/main.go`
- Modify: `go/cmd/madder-test-sftp-server/main_test.go`

**Step 1: Write the failing test**

Add to `main_test.go`:

```go
// TestHandshakeLineFormat asserts RFC 0001 section "Handshake Line":
// exactly one line on stdout, fields pipe-delimited, starts with the
// cookie, version 1, transport tcp, 127.0.0.1:PORT, known_hosts key,
// subprotocol ssh.
func TestHandshakeLineFormat(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"
	cmd := exec.Command("go", "run", ".")
	cmd.Env = []string{"MADDER_PLUGIN_COOKIE=" + cookie}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	buf := make([]byte, 1024)
	n, _ := stdout.Read(buf)
	line := strings.TrimRight(string(buf[:n]), "\n")

	fields := strings.Split(line, "|")
	if len(fields) != 6 {
		t.Fatalf("expected 6 pipe-delimited fields, got %d: %q", len(fields), line)
	}
	if fields[0] != cookie {
		t.Errorf("field[0] (cookie) = %q, want %q", fields[0], cookie)
	}
	if fields[1] != "1" {
		t.Errorf("field[1] (version) = %q, want 1", fields[1])
	}
	if fields[2] != "tcp" {
		t.Errorf("field[2] (transport) = %q, want tcp", fields[2])
	}
	if !strings.HasPrefix(fields[3], "127.0.0.1:") {
		t.Errorf("field[3] (address) = %q, want 127.0.0.1: prefix", fields[3])
	}
	if !strings.HasPrefix(fields[4], "known_hosts=") {
		t.Errorf("field[4] (metadata) = %q, want known_hosts= prefix", fields[4])
	}
	if fields[5] != "ssh" {
		t.Errorf("field[5] (subprotocol) = %q, want ssh", fields[5])
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go -run TestHandshakeLineFormat ./cmd/madder-test-sftp-server/...`
Expected: FAIL — either short-read (no handshake emitted) or field count mismatch.

**Step 3: Write minimal implementation**

Replace `main.go` with the full handshake. Port dodder's binary structure, bind `127.0.0.1:0`, generate ECDSA host key, write `known_hosts`, emit the RFC-0001 handshake line. Full reference: `~/eng/repos/dodder/go/cmd/test-sftp-server/main.go`. Critical snippet:

```go
const (
	programName     = "madder-test-sftp-server"
	protocolVersion = "1"
	subprotocol     = "ssh"
)

func main() {
	cookie := os.Getenv("MADDER_PLUGIN_COOKIE")
	if cookie == "" {
		fmt.Fprintf(os.Stderr, "[%s] magic cookie mismatch\n", programName)
		os.Exit(1)
	}

	hostKey, err := generateECDSAHostKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] host key: %v\n", programName, err)
		os.Exit(1)
	}

	knownHostsPath, err := writeKnownHosts(hostKey.PublicKey())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] known_hosts: %v\n", programName, err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] listen: %v\n", programName, err)
		os.Exit(1)
	}

	fmt.Printf(
		"%s|%s|tcp|%s|known_hosts=%s|%s\n",
		cookie,
		protocolVersion,
		listener.Addr().String(),
		knownHostsPath,
		subprotocol,
	)

	serve(listener, hostKey) // loop forever for now; stdin-close in Task 3
}
```

Helper functions `generateECDSAHostKey`, `writeKnownHosts`, `serve`, and the SSH config setup come from dodder's reference binary; port verbatim except for the `[madder-test-sftp-server]` stderr prefix and the new handshake format.

**Step 4: Run test to verify it passes**

Run: `just test-go -run TestHandshakeLineFormat ./cmd/madder-test-sftp-server/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/cmd/madder-test-sftp-server/
git commit -m "feat(madder-test-sftp-server): emit RFC-0001 handshake line"
```

---

### Task 3: Stdin-close shutdown + test

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/cmd/madder-test-sftp-server/main.go`
- Modify: `go/cmd/madder-test-sftp-server/main_test.go`

**Step 1: Write the failing test**

```go
// TestStdinCloseTriggersCleanExit asserts RFC 0001 Lifecycle:
// closing the child's stdin MUST trigger graceful shutdown with
// exit 0 within a short grace window.
func TestStdinCloseTriggersCleanExit(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"
	cmd := exec.Command("go", "run", ".")
	cmd.Env = []string{"MADDER_PLUGIN_COOKIE=" + cookie}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait for handshake so we know the server is running.
	buf := make([]byte, 1024)
	if _, err := stdout.Read(buf); err != nil {
		t.Fatal(err)
	}

	// Close stdin — the documented shutdown signal.
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("child exited with error: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not exit within 10s of stdin close")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `just test-go -run TestStdinCloseTriggersCleanExit ./cmd/madder-test-sftp-server/...`
Expected: FAIL with "child did not exit within 10s of stdin close" (server runs forever today).

**Step 3: Write minimal implementation**

Add a stdin-watcher goroutine in `main` that triggers graceful shutdown. Append to `serve` the ability to close the listener when signaled:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go func() {
	io.Copy(io.Discard, os.Stdin) // blocks until EOF
	cancel()
}()

go serve(ctx, listener, hostKey)

<-ctx.Done()
_ = listener.Close()
_ = os.Remove(knownHostsPath)
```

`serve` takes the ctx and returns when either the listener closes or ctx cancels.

**Step 4: Run test to verify it passes**

Run: `just test-go -run TestStdinCloseTriggersCleanExit ./cmd/madder-test-sftp-server/...`
Expected: PASS.

**Step 5: Commit**

```bash
git add go/cmd/madder-test-sftp-server/
git commit -m "feat(madder-test-sftp-server): stdin-close graceful shutdown"
```

---

### Task 4: Add nix derivation + devshell wiring

**Promotion criteria:** N/A.

**Files:**
- Modify: `go/default.nix`
- Modify: `flake.nix`

**Step 1: Add the derivation in go/default.nix**

Find the existing `madder` or `madder-cache` derivation. Add a sibling:

```nix
madder-test-sftp-server = buildGoModule {
  pname = "madder-test-sftp-server";
  version = "0.2.0"; # mirror main package
  src = ./.;
  vendorHash = null; # using gomod2nix
  subPackages = [ "cmd/madder-test-sftp-server" ];
  # rest mirrors madder's derivation structure
};
```

**Step 2: Wire into flake.nix devshell only**

In `flake.nix`, find the `devShells.default` block. Add `madder-test-sftp-server` to its `buildInputs`. Do **not** add it to `packages.default`, `packages.madder-test-sftp-server`, or `apps.*` — it must remain devshell-only.

**Step 3: Verify**

Run: `direnv reload` (user must do this manually — do not attempt from inside the session). Then inside the devshell:

```bash
command -v madder-test-sftp-server
```

Expected: `/nix/store/.../bin/madder-test-sftp-server`. If it resolves, the devshell wiring worked.

**Step 4: Smoke-test the binary**

```bash
MADDER_PLUGIN_COOKIE=test madder-test-sftp-server &
sleep 0.5
kill %1
```

Expected: prints the RFC-0001 handshake line to stdout, exits cleanly on SIGTERM.

**Step 5: Commit**

```bash
git add go/default.nix flake.nix
git commit -m "build(nix): devshell-only madder-test-sftp-server derivation"
```

**Note:** This task requires a direnv reload the user performs manually. If the session is still running the old devshell, subsequent tasks that need the binary on PATH will fail; ask the user to reload before continuing to Task 5.

---

### Task 5: `zz-tests_bats/lib/sftp.bash` helpers + handshake-only bats scenario

**Promotion criteria:** N/A.

**Files:**
- Create: `zz-tests_bats/lib/sftp.bash`
- Create: `zz-tests_bats/sftp.bats`

**Step 1: Write the failing scenario**

```bash
#! /usr/bin/env bats

setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp.bash"
  export output
  start_sftp_server
}

teardown() {
  stop_sftp_server
}

# bats file_tags=net_cap

function sftp_handshake_exports_port_and_known_hosts { # @test
  [[ -n $SFTP_PORT ]] || fail "SFTP_PORT not exported"
  [[ -n $SFTP_KNOWN_HOSTS ]] || fail "SFTP_KNOWN_HOSTS not exported"
  [[ -f $SFTP_KNOWN_HOSTS ]] || fail "known_hosts file missing"
}
```

**Step 2: Verify the scenario fails**

Run: `just test-bats-targets sftp.bats`
Expected: FAIL — `start_sftp_server` is undefined (lib/sftp.bash doesn't exist).

**Step 3: Create the helper**

```bash
# zz-tests_bats/lib/sftp.bash
#! /bin/bash -e

# start_sftp_server spawns madder-test-sftp-server as a coprocess per
# RFC 0001. Exports SFTP_PORT, SFTP_KNOWN_HOSTS, SFTP_PID for the test
# body. Fails loudly on handshake timeout with captured stderr.
start_sftp_server() {
  require_bin MADDER_TEST_SFTP_SERVER madder-test-sftp-server

  local cookie
  cookie="$(head -c 16 /dev/urandom | xxd -p)"

  local stderr_file="$BATS_TEST_TMPDIR/madder-test-sftp-server.stderr"

  exec {SFTP_IN}< <(
    MADDER_PLUGIN_COOKIE="$cookie" \
      "$MADDER_TEST_SFTP_SERVER" 2>"$stderr_file"
  )
  export SFTP_IN
  export SFTP_PID=$!

  local line
  if ! read -r -t 5 line <&"$SFTP_IN"; then
    local stderr_contents
    stderr_contents="$(cat "$stderr_file" 2>/dev/null || echo '<no stderr>')"
    fail "SFTP handshake timeout after 5s. stderr: $stderr_contents"
  fi

  local -a fields
  IFS='|' read -ra fields <<<"$line"
  if [[ ${#fields[@]} -ne 6 ]]; then
    fail "SFTP handshake malformed (want 6 fields, got ${#fields[@]}): $line"
  fi
  if [[ ${fields[0]} != "$cookie" ]]; then
    fail "SFTP handshake cookie mismatch: got ${fields[0]}, want $cookie"
  fi
  if [[ ${fields[1]} != "1" ]]; then
    fail "SFTP handshake version: got ${fields[1]}, want 1"
  fi

  export SFTP_PORT="${fields[3]##*:}"
  export SFTP_KNOWN_HOSTS="${fields[4]#known_hosts=}"
}

# stop_sftp_server closes stdin, signaling RFC 0001 graceful shutdown.
stop_sftp_server() {
  if [[ -n ${SFTP_IN:-} ]]; then
    exec {SFTP_IN}<&-
    unset SFTP_IN
  fi
  if [[ -n ${SFTP_PID:-} ]]; then
    wait "$SFTP_PID" 2>/dev/null || true
    unset SFTP_PID
  fi
}
```

**Step 4: Run test to verify it passes**

Run: `just test-bats-targets sftp.bats`

Expected: FAIL with "test filtered by tag !net_cap" because the default `test-bats-targets` likely inherits the new filter (if Task 6 hasn't run yet). If that's the case, skip ahead to Task 6, then return.

To bypass the filter for this verification step, run:
```bash
cd zz-tests_bats && bats --allow-local-binding sftp.bats
```
Expected: the one scenario passes.

**Step 5: Commit**

```bash
git add zz-tests_bats/lib/sftp.bash zz-tests_bats/sftp.bats
git commit -m "test(sftp): handshake helper + smoke scenario"
```

---

### Task 6: Justfile partition via `net_cap` tag

**Promotion criteria:** The existing `test-bats` recipe must continue to run the remaining 52 scenarios it ran before — no regressions in default coverage.

**Files:**
- Modify: `justfile`

**Step 1: Write the failing check**

Run: `just test-bats-net-cap`
Expected: FAIL with "Justfile does not contain recipe `test-bats-net-cap`".

**Step 2: Add the recipe + update existing composition**

In `justfile`, find `test-bats` and update:

```just
# Run bats integration tests (net_cap tests excluded).
[group("test")]
test-bats: build
    cd zz-tests_bats && bats --filter-tags '!net_cap' --jobs 8 *.bats

# Run bats tests that require loopback binding (SFTP, future HTTP, etc.).
[group("test")]
test-bats-net-cap: build
    cd zz-tests_bats && bats --allow-local-binding --filter-tags net_cap *.bats

# Update the default `test` composition.
test: test-go-race test-bats test-bats-net-cap
```

**Step 3: Verify each recipe independently**

- Run: `just test-bats`
- Expected: 52 scenarios pass, sftp.bats excluded by filter.

- Run: `just test-bats-net-cap`
- Expected: 1 sftp scenario (`sftp_handshake_exports_port_and_known_hosts`) passes.

- Run: `just test`
- Expected: everything passes.

**Step 4: Commit**

```bash
git add justfile
git commit -m "build(justfile): partition bats runs by net_cap tag"
```

---

### Task 7: `sftp_write_emits_written_record` scenario

**Promotion criteria:** N/A.

**Files:**
- Modify: `zz-tests_bats/sftp.bats`

**Step 1: Write the failing scenario**

Add to `sftp.bats`:

```bash
function sftp_write_emits_written_record { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"

  # Initialize an SFTP-backed store pointing at the test server.
  local remote_root="$BATS_TEST_TMPDIR/remote-blobs"
  mkdir -p "$remote_root"

  run_madder init \
    -encryption none \
    sftp://127.0.0.1:$SFTP_PORT/"$remote_root".sftp-test
  assert_success

  # Write a blob via the SFTP store.
  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  run_madder write --blob-store .sftp-test "$blob"
  assert_success

  # Assert one op=written record.
  local date
  date="$(date -u +%Y-%m-%d)"
  local log="$XDG_LOG_HOME/madder/blob-writes-$date.ndjson"
  [[ -s $log ]] || fail "expected write-log at $log, got none"
  local n
  n="$(grep -c '"op":"written"' "$log" || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n"
}
```

**Step 2: Verify it fails**

Run: `just test-bats-net-cap`
Expected: FAIL. Reasons vary — the exact `madder init` SFTP URL syntax may need adjustment after reading `go/internal/india/commands/init.go`. Iterate until the test passes.

**Step 3: Adjust**

Look at `go/internal/charlie/blob_store_configs/toml_sftp_v0.go` for expected SFTP URL shape and at `go/internal/india/commands/init.go` for the `init` subcommand's flag surface. Update the test to use the actual syntax. No production code changes should be required — #50 already wired the observer for the SFTP publish path.

**Step 4: Verify it passes**

Run: `just test-bats-net-cap`
Expected: 2 sftp scenarios pass.

**Step 5: Commit**

```bash
git add zz-tests_bats/sftp.bats
git commit -m "test(sftp): write emits op=written audit record"
```

---

### Task 8: `sftp_write_disabled_by_no_write_log_flag` + `sftp_write_disabled_by_env_var` scenarios

**Promotion criteria:** N/A.

**Files:**
- Modify: `zz-tests_bats/sftp.bats`

**Step 1: Write the failing scenarios**

Add both, modeled after the existing `write_log_disabled_by_*` scenarios in `zz-tests_bats/write_log.bats` but pointed at the SFTP store.

**Step 2: Verify they fail**

Run: `just test-bats-net-cap`
Expected: FAIL if the assertion catches any record slipping through. Since #50 observer wiring respects `--no-write-log` via `command_components.makeBlobWriteObserver`'s handling of `MADDER_WRITE_LOG` and the globals type-assertion, the scenarios should pass on first run; if so, the test harness validates the wiring rather than hunting a bug. Still worth running a failing version first by temporarily removing the observer disable logic to make sure the test actually catches a regression.

**Step 3: Verify they pass**

Run: `just test-bats-net-cap`
Expected: 4 sftp scenarios pass.

**Step 4: Commit**

```bash
git add zz-tests_bats/sftp.bats
git commit -m "test(sftp): write-log disable paths cover SFTP too"
```

---

### Task 9: `sftp_write_record_has_contracted_fields` scenario

**Promotion criteria:** N/A.

**Files:**
- Modify: `zz-tests_bats/sftp.bats`

**Step 1: Write the failing scenario**

Model after `write_log_record_has_contracted_fields` from `write_log.bats`, but with the SFTP store as the source of the write.

**Step 2: Verify it passes**

Run: `just test-bats-net-cap`
Expected: 5 sftp scenarios pass.

**Step 3: Commit**

```bash
git add zz-tests_bats/sftp.bats
git commit -m "test(sftp): record carries every ADR 0004 contracted field"
```

---

### Task 10: Remove the Task-5 smoke scenario

**Promotion criteria:** Task 7-9 scenarios all pass independently — the smoke scenario is redundant.

**Files:**
- Modify: `zz-tests_bats/sftp.bats`

**Step 1: Delete `sftp_handshake_exports_port_and_known_hosts`**

The Task-7-9 scenarios exercise the handshake transitively; the original smoke scenario was a stepping stone.

**Step 2: Verify**

Run: `just test-bats-net-cap`
Expected: 4 sftp scenarios pass (down from 5).

**Step 3: Commit**

```bash
git add zz-tests_bats/sftp.bats
git commit -m "test(sftp): drop redundant smoke scenario"
```

---

### Task 11: Full verification + comment on #54

**Promotion criteria:** N/A.

**Files:**
- N/A (runs tests, updates GitHub).

**Step 1: Full test suite**

Run: `just test`
Expected: `test-go-race` green, `test-bats` (52 scenarios without sftp) green, `test-bats-net-cap` (4 sftp scenarios) green.

**Step 2: Smoke each recipe independently**

Run: `just test-bats` → 52 scenarios pass.
Run: `just test-bats-net-cap` → 4 scenarios pass.
Run: `just test-go-race` → go race suite clean.

**Step 3: Comment on #54 with the final SHA**

Use `mcp__plugin_moxy_moxy__get-hubbed_issue-comment` against issue 54, body referencing the final commit SHA, the RFC it implements (0001), and the design plan it executed.

**Step 4: No final commit**

The comment is issue metadata; no code change.

---

## Execution notes

- Tasks 4 requires a devshell reload the user must trigger via `direnv reload` from a shell outside this session. If Task 5+ fails because `madder-test-sftp-server` isn't on PATH, pause and ask.
- Task 5's verification uses a raw `bats --allow-local-binding` because Task 6 hasn't landed the justfile recipe yet. This is expected.
- Tasks 7-9 may surface real SFTP-init URL syntax issues that the #50 commit message papered over (there's no bats coverage of SFTP init yet). If the tests expose actual bugs in madder's SFTP code paths, stop and confirm whether to fix in this PR or file separately.
