package main

import (
	"bytes"
	"strings"
	"testing"
)

const testSeed = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestRun_PrintsTwoRecipientLines(t *testing.T) {
	var out bytes.Buffer

	if err := runMain([]string{"-seed", testSeed}, &out); err != nil {
		t.Fatalf("runMain: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 recipient lines, got %d:\n%s", len(lines), out.String())
	}

	if !strings.HasPrefix(lines[0], "piggy-recipient-v1@age_x25519_pub-") {
		t.Errorf("line 1 is not an X25519 recipient: %q", lines[0])
	}

	if !strings.HasPrefix(lines[1], "piggy-recipient-v1@pivy_ecdh_p256_pub-") {
		t.Errorf("line 2 is not a P-256 recipient: %q", lines[1])
	}
}

// The same seed must mint the same recipients every time: the Rust tests
// derive the public key from the committed seed and compare.
func TestRun_IsDeterministic(t *testing.T) {
	var first, second bytes.Buffer

	if err := runMain([]string{"-seed", testSeed}, &first); err != nil {
		t.Fatalf("runMain: %v", err)
	}

	if err := runMain([]string{"-seed", testSeed}, &second); err != nil {
		t.Fatalf("runMain: %v", err)
	}

	if first.String() != second.String() {
		t.Errorf("non-deterministic output:\n%s\n---\n%s", first.String(), second.String())
	}

	var other bytes.Buffer

	otherSeed := "ff" + testSeed[2:]
	if err := runMain([]string{"-seed", otherSeed}, &other); err != nil {
		t.Fatalf("runMain: %v", err)
	}

	if other.String() == first.String() {
		t.Errorf("different seeds minted the same recipients")
	}
}

func TestRun_PrintSecretEchoesTheSeed(t *testing.T) {
	var out bytes.Buffer

	if err := runMain([]string{"-seed", testSeed, "-print-secret"}, &out); err != nil {
		t.Fatalf("runMain: %v", err)
	}

	if got := strings.TrimSpace(out.String()); got != testSeed {
		t.Errorf("secret = %q, want the seed %q", got, testSeed)
	}
}

func TestRunMain_RejectsBadSeeds(t *testing.T) {
	for name, argv := range map[string][]string{
		"missing":   {},
		"not hex":   {"-seed", "zz"},
		"too short": {"-seed", "0102"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runMain(argv, &bytes.Buffer{}); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
