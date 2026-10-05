//go:build test

package blob_store_configs

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	piggy_agent "code.linenisgreat.com/piggy/go/pkgs/agent"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/pivy"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ecdhReplyFraming selects how the fake agent frames its ecdh@joyent.com
// reply. The two differ only in what precedes the shared secret.
type ecdhReplyFraming int

const (
	// SSH_AGENT_SUCCESS (6), then the secret as an SSH string. The only
	// framing dewey's pivy client understood.
	ecdhReplySuccessThenSecret ecdhReplyFraming = iota

	// SSH_AGENT_EXTENSION_RESPONSE (29), then the extension name as an SSH
	// string, then the secret as an SSH string: the framing piggy's own
	// conformance client expects from the Rust piggy-agent.
	// zz-tests_bats/piv_agent.bats covers the real agent.
	ecdhReplyExtensionResponseNameThenSecret
)

const ecdhExtensionName = "ecdh@joyent.com"

// fakeECDHAgent is an SSH agent that serves only ecdh@joyent.com, doing
// the scalar multiplication a PIV slot-9D key would do on the card.
type fakeECDHAgent struct {
	agent.Agent
	priv    *ecdh.PrivateKey
	framing ecdhReplyFraming
}

func (a *fakeECDHAgent) SignWithFlags(
	ssh.PublicKey, []byte, agent.SignatureFlags,
) (*ssh.Signature, error) {
	return nil, agent.ErrExtensionUnsupported
}

func (a *fakeECDHAgent) Extension(extType string, contents []byte) ([]byte, error) {
	if extType != ecdhExtensionName {
		return nil, agent.ErrExtensionUnsupported
	}

	// The request body is one SSH string wrapping
	// string(recipient key) string(ephemeral key) uint32(flags).
	var outer struct {
		Inner []byte
	}

	if err := ssh.Unmarshal(contents, &outer); err != nil {
		return nil, err
	}

	var inner struct {
		RecipientKey []byte
		EphemeralKey []byte
		Flags        uint32
	}

	if err := ssh.Unmarshal(outer.Inner, &inner); err != nil {
		return nil, err
	}

	var ephemeral struct {
		KeyType string
		Curve   string
		Point   []byte
	}

	if err := ssh.Unmarshal(inner.EphemeralKey, &ephemeral); err != nil {
		return nil, err
	}

	ephemeralPub, err := ecdh.P256().NewPublicKey(ephemeral.Point)
	if err != nil {
		return nil, err
	}

	secret, err := a.priv.ECDH(ephemeralPub)
	if err != nil {
		return nil, err
	}

	sshString := func(b []byte) []byte {
		out := make([]byte, 4+len(b))
		binary.BigEndian.PutUint32(out, uint32(len(b)))
		copy(out[4:], b)
		return out
	}

	switch a.framing {
	case ecdhReplyExtensionResponseNameThenSecret:
		reply := []byte{29}
		reply = append(reply, sshString([]byte(ecdhExtensionName))...)
		reply = append(reply, sshString(secret)...)
		return reply, nil

	default:
		return append([]byte{6}, sshString(secret)...), nil
	}
}

// startFakeECDHAgent serves a fakeECDHAgent on a unix socket, points
// PIVY_AUTH_SOCK at it, and returns the pivy_ecdh_p256_pub markl id of
// the key it holds.
func startFakeECDHAgent(t *testing.T, framing ecdhReplyFraming) markl.Id {
	t.Helper()

	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating P-256 key: %v", err)
	}

	// Not t.TempDir(): it nests under $TMPDIR and the test name, which
	// overflows the ~108-byte sun_path limit for a unix socket.
	socketDir, err := os.MkdirTemp("/tmp", "ecdh")
	if err != nil {
		t.Fatalf("creating socket dir: %v", err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })

	socketPath := filepath.Join(socketDir, "agent.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listening on %s: %v", socketPath, err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	fake := &fakeECDHAgent{
		Agent:   agent.NewKeyring(),
		priv:    priv,
		framing: framing,
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go func() {
				defer conn.Close() //defer:err-checked
				_ = agent.ServeAgent(fake, conn)
			}()
		}
	}()

	// PIGGY_AUTH_SOCK is first in piggy's lookup order, ahead of
	// SSH_AUTH_SOCK, so the test never reaches a developer's real agent.
	t.Setenv("PIGGY_AUTH_SOCK", socketPath)

	var id markl.Id

	if err := id.SetMarklId(
		markl.FormatIdPivyEcdhP256Pub,
		pivy.CompressP256Point(priv.PublicKey()),
	); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}

	return id
}

// roundTripThroughStoreEncryption encrypts and decrypts plaintext with the
// IO wrapper a store configured with `encryption = [id]` would use.
func roundTripThroughStoreEncryption(
	t *testing.T,
	id markl.Id,
	plaintext []byte,
) (decrypted []byte, err error) {
	t.Helper()

	ioWrapper, err := EncryptionKeys{id}.GetIOWrapper()
	if err != nil {
		t.Fatalf("GetIOWrapper: %v", err)
	}

	var ciphertext bytes.Buffer

	writer, err := ioWrapper.WrapWriter(&ciphertext)
	if err != nil {
		t.Fatalf("WrapWriter: %v", err)
	}

	if _, err = writer.Write(plaintext); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if bytes.Contains(ciphertext.Bytes(), plaintext) {
		t.Fatalf("ciphertext contains the plaintext")
	}

	reader, err := ioWrapper.WrapReader(&ciphertext)
	if err != nil {
		return nil, err
	}

	defer reader.Close() //defer:err-checked

	return io.ReadAll(reader)
}

// A single-PIV-recipient store round-trips against an agent using either
// reply framing. The name-first framing is the regression guard: with
// dewey's ECDH client (piggy before 78ce2ef) it failed to decrypt,
// because that client took the first SSH string of the reply, the
// extension name, as the shared secret. piggy's own client, which the
// pivy_ecdh_p256_pub wrapper now uses, accepts both.
func TestPivyRecipient_RoundTrips(t *testing.T) {
	for name, framing := range map[string]ecdhReplyFraming{
		"success then secret":                  ecdhReplySuccessThenSecret,
		"extension response, name then secret": ecdhReplyExtensionResponseNameThenSecret,
	} {
		t.Run(name, func(t *testing.T) {
			id := startFakeECDHAgent(t, framing)
			plaintext := []byte("blob bytes for the pivy round trip")

			decrypted, err := roundTripThroughStoreEncryption(t, id, plaintext)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}

			if !bytes.Equal(decrypted, plaintext) {
				t.Fatalf("decrypted %q, want %q", decrypted, plaintext)
			}
		})
	}
}

// Encrypting to a PIV recipient is software-only, so building the
// wrapper and writing must work with no agent socket configured at all.
// Before piggy 78ce2ef the wrapper resolved the socket at construction
// and failed here.
func TestPivyRecipient_EncryptsWithoutAnAgent(t *testing.T) {
	id := startFakeECDHAgent(t, ecdhReplySuccessThenSecret)

	for _, name := range []string{
		"PIGGY_AUTH_SOCK", "SSH_AUTH_SOCK", "PIVY_AUTH_SOCK",
	} {
		t.Setenv(name, "")
	}

	ioWrapper, err := EncryptionKeys{id}.GetIOWrapper()
	if err != nil {
		t.Fatalf("GetIOWrapper with no agent socket set: %v", err)
	}

	var ciphertext bytes.Buffer

	writer, err := ioWrapper.WrapWriter(&ciphertext)
	if err != nil {
		t.Fatalf("WrapWriter: %v", err)
	}

	if _, err = writer.Write([]byte("written with no agent")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Decrypting without an agent must fail as an AGENT error, so the
	// blob reader surfaces it instead of treating it as a cleartext blob.
	_, err = ioWrapper.WrapReader(&ciphertext)
	if !piggy_agent.IsErrAgent(err) {
		t.Fatalf("WrapReader with no agent: got %v, want an agent error", err)
	}
}
