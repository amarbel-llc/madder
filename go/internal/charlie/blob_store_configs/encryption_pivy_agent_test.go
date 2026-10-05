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

	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/pivy"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// ecdhReplyFraming selects how the fake agent frames its ecdh@joyent.com
// reply. The two differ only in what precedes the shared secret.
type ecdhReplyFraming int

const (
	// SSH_AGENT_SUCCESS (6), then the secret as an SSH string. This is the
	// framing dewey's pivy client parses (pivy/agent.go parseECDHResponse).
	ecdhReplySuccessThenSecret ecdhReplyFraming = iota

	// SSH_AGENT_EXTENSION_RESPONSE (29), then the extension name as an SSH
	// string, then the secret as an SSH string. This is the framing
	// piggy-agent is reported to send (piggy session reading of
	// crates/piggy/src/cmd/agent/session.rs; not observed on a live agent
	// by this test).
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

	t.Setenv("PIVY_AUTH_SOCK", socketPath)

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

// The control: against an agent that frames its reply the way dewey's
// client expects, a single-PIV-recipient store round-trips. This is the
// first end-to-end exercise of that path in madder, and it shows the
// fake agent and the request encoding are sound, so the failure in the
// next test is attributable to the reply framing alone.
func TestPivyRecipient_RoundTrips_SuccessThenSecretFraming(t *testing.T) {
	id := startFakeECDHAgent(t, ecdhReplySuccessThenSecret)
	plaintext := []byte("blob bytes for the pivy round trip")

	decrypted, err := roundTripThroughStoreEncryption(t, id, plaintext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted %q, want %q", decrypted, plaintext)
	}
}

// CHARACTERIZATION TEST: pins a defect, not a contract.
//
// Against an agent that answers with SSH_AGENT_EXTENSION_RESPONSE plus
// the extension name before the secret, the same store cannot decrypt
// what it just encrypted. dewey's parseECDHResponse skips one byte and
// takes the first SSH string as the shared secret; under this framing
// that string is the 15-byte extension name, so the derived wrapping key
// is wrong and the unwrap fails.
//
// If piggy-agent does frame its reply this way, every madder store
// encrypted to a single pivy_ecdh_p256_pub recipient is unreadable
// through it. The fix belongs in dewey's pivy client. When it lands this
// test should FAIL: flip it to assert a successful round trip.
func TestPivyRecipient_CannotDecrypt_ExtensionResponseFraming(t *testing.T) {
	id := startFakeECDHAgent(t, ecdhReplyExtensionResponseNameThenSecret)
	plaintext := []byte("blob bytes for the pivy round trip")

	decrypted, err := roundTripThroughStoreEncryption(t, id, plaintext)
	if err == nil {
		t.Fatalf(
			"decrypt succeeded (%q): dewey's ECDH client now handles the "+
				"extension-response framing; rewrite this test as a round trip",
			decrypted,
		)
	}

	t.Logf("decrypt failed as characterized: %v", err)
}
