//go:build test

package blob_stores

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// makeHostKeyCallback should pick up known_hosts from $SSH_HOME (XDG-style
// SSH config dir) when the legacy ~/.ssh/known_hosts location is absent or
// stale. Repro for the rsync.net fsck failure where $SSH_HOME pointed at
// ~/.config/ssh but the SFTP store only ever read ~/.ssh/known_hosts.
func TestMakeHostKeyCallback_RespectsSSHHomeEnv(t *testing.T) {
	sshHome := t.TempDir()

	// A minimal valid known_hosts line — knownhosts.New parses the file
	// at construction time and rejects malformed content, so we need a
	// real entry, not just an empty file.
	knownHostsLine := "example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM7v3yz1J4VqZqZqZqZqZqZqZqZqZqZqZqZqZqZqZqZq\n"

	knownHostsPath := filepath.Join(sshHome, "known_hosts")
	if err := os.WriteFile(knownHostsPath, []byte(knownHostsLine), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	t.Setenv("SSH_HOME", sshHome)

	cb, files, err := makeHostKeyCallback("")
	if err != nil {
		t.Fatalf("makeHostKeyCallback returned error: %v", err)
	}
	if cb == nil {
		t.Fatal("makeHostKeyCallback returned nil callback without error")
	}

	// The returned files slice powers the "verifying host key against
	// known_hosts: [...]" log line; the SSH_HOME path must appear so
	// debuggers can see at a glance which files were actually loaded.
	found := false
	for _, f := range files {
		if f == knownHostsPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected returned files to include %q; got %v", knownHostsPath, files)
	}
}

// hostKeyAlgorithmsForKnownHosts must enumerate every algorithm recorded
// for the target host so ssh.ClientConfig.HostKeyAlgorithms can constrain
// negotiation to types we have a verified entry for. Repro for issue #99:
// when a server offered RSA + ECDSA + ed25519 but known_hosts only had the
// ed25519 entry, Go negotiated RSA and the callback returned `key mismatch`
// rather than picking the algorithm we trusted.
func TestHostKeyAlgorithmsForKnownHosts_EnumeratesRecordedAlgorithms(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")

	hosts := []string{"example.com"}

	// Two real keys, two algorithms — keeps the test fast while still
	// proving the helper returns more than one entry. Generating live keys
	// avoids hardcoding production fingerprints in test fixtures.
	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519: %v", err)
	}
	edPub, err := ssh.NewPublicKey(edPriv.Public())
	if err != nil {
		t.Fatalf("wrap ed25519 pub: %v", err)
	}

	ecPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa: %v", err)
	}
	ecPub, err := ssh.NewPublicKey(&ecPriv.PublicKey)
	if err != nil {
		t.Fatalf("wrap ecdsa pub: %v", err)
	}

	body := knownhosts.Line(hosts, edPub) + "\n" +
		knownhosts.Line(hosts, ecPub) + "\n"

	if err := os.WriteFile(khPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cb, err := knownhosts.New(khPath)
	if err != nil {
		t.Fatalf("knownhosts.New: %v", err)
	}

	algos := hostKeyAlgorithmsForKnownHosts(cb, "example.com:22")
	sort.Strings(algos)

	want := []string{ecPub.Type(), edPub.Type()}
	sort.Strings(want)

	if len(algos) != len(want) {
		t.Fatalf("algorithm count: got %v, want %v", algos, want)
	}
	for i, a := range algos {
		if a != want[i] {
			t.Fatalf("algorithms differ at index %d: got %v, want %v", i, algos, want)
		}
	}
}

// When the host has no entry in known_hosts at all, the helper must return
// nil so callers leave ssh.ClientConfig.HostKeyAlgorithms unset and Go's
// default algorithm preference applies (preserving today's first-connection
// behavior — modulo TOFU which is already absent).
func TestHostKeyAlgorithmsForKnownHosts_UnknownHostReturnsNil(t *testing.T) {
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")

	_, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519: %v", err)
	}
	edPub, err := ssh.NewPublicKey(edPriv.Public())
	if err != nil {
		t.Fatalf("wrap ed25519 pub: %v", err)
	}

	body := knownhosts.Line([]string{"example.com"}, edPub) + "\n"
	if err := os.WriteFile(khPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cb, err := knownhosts.New(khPath)
	if err != nil {
		t.Fatalf("knownhosts.New: %v", err)
	}

	algos := hostKeyAlgorithmsForKnownHosts(cb, "different-host.example:22")
	if len(algos) != 0 {
		t.Fatalf("expected nil/empty algos for unknown host, got %v", algos)
	}
}
