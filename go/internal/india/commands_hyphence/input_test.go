package commands_hyphence

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func TestOpenInput_FilePath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "hyphence-input-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("hello"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r, source, closer, err := OpenInput(f.Name(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closer.Close()
	if source != f.Name() {
		t.Errorf("source mismatch: got %q, want %q", source, f.Name())
	}
	got, _ := io.ReadAll(r)
	if string(got) != "hello" {
		t.Errorf("content mismatch: got %q, want %q", got, "hello")
	}
}

func TestOpenInput_Stdin(t *testing.T) {
	stdin := strings.NewReader("piped")
	r, source, closer, err := OpenInput("-", stdin)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer closer.Close()
	if source != "-" {
		t.Errorf("source for stdin should be '-', got %q", source)
	}
	got, _ := io.ReadAll(r)
	if string(got) != "piped" {
		t.Errorf("content mismatch: got %q, want %q", got, "piped")
	}
}

func TestOpenInput_FileNotFound(t *testing.T) {
	_, _, _, err := OpenInput("/nonexistent/path/xyz", nil)
	if err == nil {
		t.Fatalf("expected error for nonexistent path, got nil")
	}
	var noInput *NoInputError
	if !errors.As(err, &noInput) {
		t.Errorf("expected *NoInputError, got %T: %v", err, err)
	}
}
