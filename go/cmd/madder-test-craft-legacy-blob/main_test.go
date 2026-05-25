package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
)

func TestRun_NoneNoneStdin(t *testing.T) {
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

func TestRun_CompressionMagicBytes(t *testing.T) {
	cases := []struct {
		comp     string
		wantHead []byte
	}{
		{"zstd", []byte{0x28, 0xb5, 0x2f, 0xfd}},
		{"gzip", []byte{0x1f, 0x8b}},
		{"zlib", []byte{0x78}},
	}
	for _, tc := range cases {
		t.Run(tc.comp, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "blob.bin")

			stdin := bytes.NewReader([]byte(
				"hello craft — non-trivial content for compression to chew on",
			))
			if err := run(tc.comp, "none", "", "-", out, stdin); err != nil {
				t.Fatalf("run: %v", err)
			}

			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(got, tc.wantHead) {
				t.Errorf("output does not start with %s magic: got % x",
					tc.comp, got[:minInt(8, len(got))])
			}
		})
	}
}

func TestRun_AgeEncrypted(t *testing.T) {
	dir := t.TempDir()

	var key markl.Id
	if err := key.GeneratePrivateKey(
		nil,
		markl.FormatIdAgeX25519Sec,
		markl.PurposeMadderPrivateKeyV1,
	); err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	keyPath := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(keyPath, []byte(key.StringWithFormat()+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := filepath.Join(dir, "blob.bin")
	stdin := bytes.NewReader([]byte("secret payload"))

	if err := run("zstd", "age", keyPath, "-", out, stdin); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, []byte("age-encryption.org/v1\n")) {
		t.Errorf("output does not start with age v1 header: got %q",
			string(got[:minInt(40, len(got))]))
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
