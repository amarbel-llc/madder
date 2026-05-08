package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRun_PassthroughStdin(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "blob.bin")

	stdin := bytes.NewReader([]byte("hello craft"))
	if err := run("none", "none", "", "-", out, stdin); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello craft" {
		t.Errorf("got %q, want %q", got, "hello craft")
	}
}

func TestRunMain_RequiresOut(t *testing.T) {
	err := runMain([]string{"-compression", "none"})
	if err == nil {
		t.Fatal("expected error when -out is missing")
	}
}
