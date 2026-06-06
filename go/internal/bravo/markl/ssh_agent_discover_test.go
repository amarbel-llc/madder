//go:build test

package markl

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

const testAgentSockEnvVar = "MADDER_TEST_AGENT_SOCK"

// testAgentSocketPath returns a socket path short enough for the unix
// socket path limit (~104 bytes), falling back to /tmp when TMPDIR is
// deep (e.g. worktree-local .tmp dirs).
func testAgentSocketPath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	socket := filepath.Join(dir, "agent.sock")

	if len(socket) < 100 {
		return socket
	}

	dir, err := os.MkdirTemp("/tmp", "markl-agent")
	if err != nil {
		t.Fatalf("failed to create short temp dir: %s", err)
	}

	t.Cleanup(func() { os.RemoveAll(dir) })

	return filepath.Join(dir, "agent.sock")
}

func serveTestAgent(t *testing.T, keys ...agent.AddedKey) string {
	t.Helper()

	keyring := agent.NewKeyring()

	for _, key := range keys {
		if err := keyring.Add(key); err != nil {
			t.Fatalf("failed to add key to test agent: %s", err)
		}
	}

	socket := testAgentSocketPath(t)

	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("failed to listen on test agent socket: %s", err)
	}

	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go agent.ServeAgent(keyring, conn)
		}
	}()

	return socket
}

func TestDiscoverAgentEd25519KeysVerboseFromEnvVar(t1 *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t1.Fatalf("failed to generate key: %s", err)
	}

	socket := serveTestAgent(t1, agent.AddedKey{
		PrivateKey: priv,
		Comment:    "test-ed25519",
	})

	t1.Setenv(testAgentSockEnvVar, socket)
	// prove discovery reads the requested env var, not the default
	t1.Setenv("SSH_AUTH_SOCK", "")

	ui.RunTestContext(t1, func(t *ui.TestContext) {
		discovered, err := DiscoverAgentEd25519KeysVerbose(testAgentSockEnvVar)
		t.AssertNoError(err)

		if len(discovered) != 1 {
			t.Fatalf("expected 1 discovered key, got %d", len(discovered))
		}

		var expected Id
		t.AssertNoError(expected.SetMarklId(FormatIdEd25519SSH, []byte(pub)))
		t.AssertNoError(AssertEqual(expected, discovered[0].Id))

		if discovered[0].Comment != "test-ed25519" {
			t.Fatalf("expected comment %q, got %q", "test-ed25519", discovered[0].Comment)
		}
	})
}

func TestDiscoverAgentECDHKeysVerboseFromEnvVar(t1 *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t1.Fatalf("failed to generate key: %s", err)
	}

	socket := serveTestAgent(t1, agent.AddedKey{
		PrivateKey: priv,
		Comment:    "test-p256",
	})

	t1.Setenv(testAgentSockEnvVar, socket)
	t1.Setenv("SSH_AUTH_SOCK", "")

	ui.RunTestContext(t1, func(t *ui.TestContext) {
		discovered, err := DiscoverAgentECDHKeysVerbose(testAgentSockEnvVar)
		t.AssertNoError(err)

		if len(discovered) != 1 {
			t.Fatalf("expected 1 discovered key, got %d", len(discovered))
		}

		if discovered[0].KeyType != ssh.KeyAlgoECDSA256 {
			t.Fatalf(
				"expected key type %q, got %q",
				ssh.KeyAlgoECDSA256,
				discovered[0].KeyType,
			)
		}
	})
}

func TestDiscoverAgentKeysVerboseUnsetEnvVar(t1 *testing.T) {
	t1.Setenv(testAgentSockEnvVar, "")

	ui.RunTestContext(t1, func(t *ui.TestContext) {
		_, err := DiscoverAgentEd25519KeysVerbose(testAgentSockEnvVar)
		if err == nil {
			t.Fatal("expected error for unset env var")
		}
	})
}

// regression guard: the parameterless SSH functions still read
// SSH_AUTH_SOCK.
func TestDiscoverSSHAgentEd25519KeysVerboseDefaultsToSSHAuthSock(t1 *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t1.Fatalf("failed to generate key: %s", err)
	}

	socket := serveTestAgent(t1, agent.AddedKey{
		PrivateKey: priv,
		Comment:    "test-default",
	})

	t1.Setenv("SSH_AUTH_SOCK", socket)

	ui.RunTestContext(t1, func(t *ui.TestContext) {
		discovered, err := DiscoverSSHAgentEd25519KeysVerbose()
		t.AssertNoError(err)

		if len(discovered) != 1 {
			t.Fatalf("expected 1 discovered key, got %d", len(discovered))
		}

		var expected Id
		t.AssertNoError(expected.SetMarklId(FormatIdEd25519SSH, []byte(pub)))
		t.AssertNoError(AssertEqual(expected, discovered[0].Id))
	})
}
