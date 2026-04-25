//go:build test

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binaryPath is the path to a freshly-built madder-test-sftp-server
// binary, populated by TestMain. We build once and exec directly
// rather than `go run` because go run wraps the real binary in a
// parent process whose Kill does not propagate, leaving stderr-copy
// goroutines blocked forever in cmd.Wait().
var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "madder-test-sftp-server-build-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	binaryPath = filepath.Join(tmpDir, "madder-test-sftp-server")
	build := exec.Command("go", "build", "-tags", "test", "-o", binaryPath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic("build failed: " + err.Error())
	}

	os.Exit(m.Run())
}

// TestCookieMismatchExitsOne asserts RFC 0001's Cookie Envelope
// normative requirement: without MADDER_PLUGIN_COOKIE set, the binary
// MUST print "[<name>] magic cookie mismatch" to stderr and exit 1
// with no stdout output.
func TestCookieMismatchExitsOne(t *testing.T) {
	cmd := exec.Command(binaryPath)
	cmd.Env = envWithoutCookie()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v\nstderr: %s", err, err, stderr.String())
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

// TestHandshakeLineFormat asserts RFC 0001 section "Handshake Line":
// exactly one line on stdout, fields pipe-delimited, starts with the
// cookie, version 1, transport tcp, 127.0.0.1:PORT, known_hosts key,
// subprotocol ssh.
func TestHandshakeLineFormat(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"
	cmd := exec.Command(binaryPath)
	cmd.Env = append(envWithoutCookie(), "MADDER_PLUGIN_COOKIE="+cookie)
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
	if line == "" {
		t.Fatal("no handshake line read")
	}

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

func envWithoutCookie() []string {
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "MADDER_PLUGIN_COOKIE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
