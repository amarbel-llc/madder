//go:build test

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// binaryPath is the path to a freshly-built madder-test-webdav-server
// binary, populated by TestMain. We build once and exec directly
// rather than `go run` because go run wraps the real binary in a
// parent process whose Kill does not propagate, leaving stderr-copy
// goroutines blocked forever in cmd.Wait().
var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "madder-test-webdav-server-build-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	binaryPath = filepath.Join(tmpDir, "madder-test-webdav-server")
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
	if !strings.HasPrefix(stderr.String(), "[madder-test-webdav-server] magic cookie mismatch") {
		t.Errorf("stderr = %q, want [madder-test-webdav-server] magic cookie mismatch prefix", stderr.String())
	}
}

// TestHandshakeLineFormat asserts RFC 0001 section "Handshake Line":
// exactly one line on stdout, fields pipe-delimited, starts with the
// cookie, version 1, transport tcp, 127.0.0.1:PORT, empty metadata
// (WebDAV has no reserved keys today), subprotocol http.
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
	rootPath, ok := metadataValue(fields[4], "root")
	if !ok {
		t.Errorf("field[4] (metadata) = %q, want root= key", fields[4])
	} else if info, err := os.Stat(rootPath); err != nil {
		t.Errorf("root path %q does not exist: %v", rootPath, err)
	} else if !info.IsDir() {
		t.Errorf("root path %q is not a directory", rootPath)
	}
	if fields[5] != "http" {
		t.Errorf("field[5] (subprotocol) = %q, want http", fields[5])
	}
}

// metadataValue parses an RFC 0001 subprotocol_metadata field
// (`k=v&k=v` form) and returns the value for `key`. Returns ok=false
// if the key is absent.
func metadataValue(metadata, key string) (string, bool) {
	if metadata == "" {
		return "", false
	}
	for _, kv := range strings.Split(metadata, "&") {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		if k == key {
			return v, true
		}
	}
	return "", false
}

// TestHandshakeLineFormat_TLS pins the TLS-mode handshake: subprotocol
// is "https", metadata is "cert=<path>", and the cert file exists at
// the advertised path so the bats helper can pass it as -tls-ca-path.
//
// Holds stdin open via a pipe so the server's stdin-EOF-driven
// shutdown doesn't race with the Stat call. Without this, exec's
// default of attaching the child's stdin to /dev/null causes the
// server to receive immediate EOF, cancel its context, and remove
// the cert file before the test reads it.
func TestHandshakeLineFormat_TLS(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"
	cmd := exec.Command(binaryPath, "-tls")
	cmd.Env = append(envWithoutCookie(), "MADDER_PLUGIN_COOKIE="+cookie)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close() //nolint:errcheck
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
	if fields[5] != "https" {
		t.Errorf("field[5] (subprotocol) = %q, want https", fields[5])
	}
	certPath, ok := metadataValue(fields[4], "cert")
	if !ok {
		t.Errorf("field[4] (metadata) = %q, want cert= key", fields[4])
	} else if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert path %q does not exist: %v", certPath, err)
	}
	rootPath, ok := metadataValue(fields[4], "root")
	if !ok {
		t.Errorf("field[4] (metadata) = %q, want root= key", fields[4])
	} else if info, err := os.Stat(rootPath); err != nil {
		t.Errorf("root path %q does not exist: %v", rootPath, err)
	} else if !info.IsDir() {
		t.Errorf("root path %q is not a directory", rootPath)
	}
}

// TestStdinCloseTriggersCleanExit asserts RFC 0001 Lifecycle:
// closing the child's stdin MUST trigger graceful shutdown with
// exit 0 within a short grace window.
func TestStdinCloseTriggersCleanExit(t *testing.T) {
	const cookie = "0123456789abcdef0123456789abcdef"
	cmd := exec.Command(binaryPath)
	cmd.Env = append(envWithoutCookie(), "MADDER_PLUGIN_COOKIE="+cookie)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Wait for handshake so we know the server is running.
	buf := make([]byte, 1024)
	if _, err := stdout.Read(buf); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("read handshake: %v\nstderr: %s", err, stderr.String())
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
			t.Errorf("child exited with error: %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not exit within 10s of stdin close")
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
