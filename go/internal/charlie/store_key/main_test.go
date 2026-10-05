//go:build test

package store_key

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
	"io"
	"testing"

	"code.linenisgreat.com/piggy/go/pkgs/agent"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

// softwareRecipient is an X25519 pigpen recipient whose secret the test
// holds, standing in for a card: its opener needs no agent.
type softwareRecipient struct {
	id       markl.Id
	identity pigpen.X25519Identity
}

func makeSoftwareRecipient(t *testing.T) softwareRecipient {
	t.Helper()

	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating recipient key: %v", err)
	}

	var id markl.Id

	if err := id.SetMarklId(
		markl.FormatIdAgeX25519Pub,
		private.PublicKey().Bytes(),
	); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}

	return softwareRecipient{
		id: id,
		identity: pigpen.X25519Identity{
			Public: private.PublicKey().Bytes(),
			Secret: private.Bytes(),
		},
	}
}

func (recipient softwareRecipient) opener() Opener {
	return func(doc *pigpen.Document) ([]byte, error) {
		return doc.Open(nil, []pigpen.X25519Identity{recipient.identity})
	}
}

func encrypt(t *testing.T, wrapper interfaces.IOWrapper, plaintext string) []byte {
	t.Helper()

	var ciphertext bytes.Buffer

	writer, err := wrapper.WrapWriter(&ciphertext)
	if err != nil {
		t.Fatalf("WrapWriter: %v", err)
	}

	if _, err = io.WriteString(writer, plaintext); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err = writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	return ciphertext.Bytes()
}

func decrypt(wrapper interfaces.IOWrapper, ciphertext []byte) (string, error) {
	reader, err := wrapper.WrapReader(bytes.NewReader(ciphertext))
	if err != nil {
		return "", err
	}

	defer reader.Close() //defer:err-checked

	plaintext, err := io.ReadAll(reader)

	return string(plaintext), err
}

// Writes need neither the sealed key nor an opener; the first read loads
// and opens exactly once however many blobs follow.
func TestIOWrapper_RoundTripUnsealsOnce(t *testing.T) {
	recipient := makeSoftwareRecipient(t)

	public, sealed, err := Mint([]markl.Id{recipient.id})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	var loads, opens int

	wrapper, err := MakeIOWrapper(
		public,
		func() ([]byte, error) { loads++; return sealed, nil },
		func(doc *pigpen.Document) ([]byte, error) {
			opens++
			return recipient.opener()(doc)
		},
	)
	if err != nil {
		t.Fatalf("MakeIOWrapper: %v", err)
	}

	first := encrypt(t, wrapper, "first blob")
	second := encrypt(t, wrapper, "second blob")

	if loads != 0 || opens != 0 {
		t.Fatalf("writing touched the sealed key: loads=%d opens=%d", loads, opens)
	}

	if bytes.Contains(first, []byte("first blob")) {
		t.Fatalf("ciphertext contains the plaintext")
	}

	for ciphertext, want := range map[string]string{
		string(first):  "first blob",
		string(second): "second blob",
	} {
		got, err := decrypt(wrapper, []byte(ciphertext))
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}

		if got != want {
			t.Fatalf("decrypted %q, want %q", got, want)
		}
	}

	if loads != 1 || opens != 1 {
		t.Fatalf("two reads: loads=%d opens=%d, want 1 and 1", loads, opens)
	}
}

// Resealing changes who can open the key, not the key: blobs written
// before the reseal still decrypt, the new recipient can open the new
// document, and the removed one cannot.
func TestReseal_KeepsStoreKeyChangesRecipients(t *testing.T) {
	before := makeSoftwareRecipient(t)
	after := makeSoftwareRecipient(t)

	public, sealed, err := Mint([]markl.Id{before.id})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	original, err := MakeIOWrapper(
		public,
		func() ([]byte, error) { return sealed, nil },
		before.opener(),
	)
	if err != nil {
		t.Fatalf("MakeIOWrapper: %v", err)
	}

	ciphertext := encrypt(t, original, "written before the reseal")

	resealed, err := Reseal(sealed, before.opener(), []markl.Id{after.id})
	if err != nil {
		t.Fatalf("Reseal: %v", err)
	}

	recipients, err := SealedRecipients(resealed)
	if err != nil {
		t.Fatalf("SealedRecipients: %v", err)
	}

	if !pigpen.SameRecipientSet(recipients, []markl.Id{after.id}) {
		t.Fatalf("resealed to %v, want only the new recipient", recipients)
	}

	afterWrapper, err := MakeIOWrapper(
		public,
		func() ([]byte, error) { return resealed, nil },
		after.opener(),
	)
	if err != nil {
		t.Fatalf("MakeIOWrapper: %v", err)
	}

	got, err := decrypt(afterWrapper, ciphertext)
	if err != nil {
		t.Fatalf("new recipient decrypting an old blob: %v", err)
	}

	if got != "written before the reseal" {
		t.Fatalf("decrypted %q", got)
	}

	removedWrapper, err := MakeIOWrapper(
		public,
		func() ([]byte, error) { return resealed, nil },
		before.opener(),
	)
	if err != nil {
		t.Fatalf("MakeIOWrapper: %v", err)
	}

	if _, err = decrypt(removedWrapper, ciphertext); err == nil {
		t.Fatalf("removed recipient opened the resealed key")
	}
}

// A sealed key that opens cleanly but belongs to another store is caught
// when it is opened, with an error that says so.
func TestIOWrapper_RejectsAnotherStoresKey(t *testing.T) {
	recipient := makeSoftwareRecipient(t)

	public, _, err := Mint([]markl.Id{recipient.id})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	_, otherSealed, err := Mint([]markl.Id{recipient.id})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	wrapper, err := MakeIOWrapper(
		public,
		func() ([]byte, error) { return otherSealed, nil },
		recipient.opener(),
	)
	if err != nil {
		t.Fatalf("MakeIOWrapper: %v", err)
	}

	ciphertext := encrypt(t, wrapper, "blob")

	_, err = decrypt(wrapper, ciphertext)
	if err == nil || !bytes.Contains(
		[]byte(err.Error()),
		[]byte("does not match the store's public key"),
	) {
		t.Fatalf("got %v, want a public-key mismatch error", err)
	}
}

// An agent that cannot be reached must surface as an agent error from
// WrapReader. The blob reader relies on that to report the blob as
// unreadable rather than treating it as cleartext or corrupt.
func TestIOWrapper_AgentFailureStaysAnAgentError(t *testing.T) {
	recipient := makeSoftwareRecipient(t)

	public, sealed, err := Mint([]markl.Id{recipient.id})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	wrapper, err := MakeIOWrapper(
		public,
		func() ([]byte, error) { return sealed, nil },
		func(*pigpen.Document) ([]byte, error) {
			return nil, fmt.Errorf("no socket: %w", agent.ErrAgent)
		},
	)
	if err != nil {
		t.Fatalf("MakeIOWrapper: %v", err)
	}

	ciphertext := encrypt(t, wrapper, "blob")

	if _, err = decrypt(wrapper, ciphertext); !agent.IsErrAgent(err) {
		t.Fatalf("got %v, want an agent error", err)
	}
}

// With no socket variable set, the production opener itself reports an
// agent error.
func TestAgentOpener_NoSocketIsAnAgentError(t *testing.T) {
	for _, name := range []string{
		"PIGGY_AUTH_SOCK", "SSH_AUTH_SOCK", "PIVY_AUTH_SOCK",
	} {
		t.Setenv(name, "")
	}

	recipient := makeSoftwareRecipient(t)

	_, sealed, err := Mint([]markl.Id{recipient.id})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	doc, err := pigpen.ParseDocument(sealed)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}

	if _, err = AgentOpener(doc); !agent.IsErrAgent(err) {
		t.Fatalf("got %v, want an agent error", err)
	}
}

// The recipient-set digest ignores order and duplicates, and changes when
// a recipient is added: the properties drift detection relies on.
func TestRecipientSetDigest(t *testing.T) {
	a := makeSoftwareRecipient(t).id
	b := makeSoftwareRecipient(t).id
	c := makeSoftwareRecipient(t).id

	digest := func(ids ...markl.Id) string {
		t.Helper()

		got, err := RecipientSetDigest(ids)
		if err != nil {
			t.Fatalf("RecipientSetDigest: %v", err)
		}

		return got.String()
	}

	if digest(a, b) != digest(b, a, a) {
		t.Errorf("digest depends on order or duplicates")
	}

	if digest(a, b) == digest(a, b, c) {
		t.Errorf("digest unchanged after adding a recipient")
	}

	if digest(a, b) == digest(a) {
		t.Errorf("digest unchanged after removing a recipient")
	}
}

// A key sealed to nobody could never be opened, so minting refuses.
func TestMint_RefusesNoRecipients(t *testing.T) {
	if _, _, err := Mint(nil); err == nil {
		t.Fatalf("Mint with no recipients succeeded")
	}
}
