//go:build test

package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCookieMismatchExitsOne asserts RFC 0001's Cookie Envelope
// normative requirement: without MADDER_PLUGIN_COOKIE set, the binary
// MUST print "[<name>] magic cookie mismatch" to stderr and exit 1
// with no stdout output.
func TestCookieMismatchExitsOne(t *testing.T) {
	cmd := exec.Command("go", "run", "-tags", "test", ".")
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
