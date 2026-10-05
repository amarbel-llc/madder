// Package store_key implements the pigpen-sealed store key of FDR 0011.
//
// A store has one X25519 store key. Blobs are ordinary age files encrypted
// to its public half, so writing needs no agent and no secret. The secret
// half is never stored in the clear: it is the 32-byte payload of a sealed
// pigpen-v1 document, sealed to the recipients of the operator's pigpen.
// Reading opens that document once per process (one agent ECDH call for a
// PIV recipient) and decrypts every blob in software after that.
//
// This package is the crypto and the IO wrapper only. Where the sealed
// document is stored, and how a store's config names it, belong to the
// config and store layers.
package store_key

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	// Activates the age and pivy format registrations this package's
	// GetIOWrapper calls depend on (see madder#278).
	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/piggy/go/pkgs/agent"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen"
	"code.linenisgreat.com/piggy/go/pkgs/pigpen_resolve"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

// secretSize is the length of an X25519 scalar, and so of the sealed
// payload.
const secretSize = 32

// SealedKeyLoader returns the bytes of the store's sealed pigpen-v1
// document. It is called at most once per IOWrapper, on the first read.
type SealedKeyLoader func() ([]byte, error)

// Opener recovers a sealed document's payload. The production opener asks
// the agent; tests supply software identities.
type Opener func(doc *pigpen.Document) ([]byte, error)

// AgentOpener opens a sealed document through the key agent, resolving
// the socket (PIGGY_AUTH_SOCK, SSH_AUTH_SOCK, PIVY_AUTH_SOCK) only when
// called. A failure to reach the agent satisfies agent.IsErrAgent.
func AgentOpener(doc *pigpen.Document) ([]byte, error) {
	socketPath, err := agent.ResolveAuthSock()
	if err != nil {
		return nil, err
	}

	return doc.Open(agent.AgentECDHOracle{SocketPath: socketPath}, nil)
}

// Mint generates a fresh X25519 store key and seals its secret half to
// recipients. It returns the public half as an age_x25519_pub markl id,
// for the store config, and the sealed document's text.
func Mint(recipients []markl.Id) (public markl.Id, sealed []byte, err error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return public, nil, fmt.Errorf("generating store key: %w", err)
	}

	secret := private.Bytes()
	defer zero(secret)

	if err = public.SetPurposeId(markl.PurposePiggyRecipientV1); err != nil {
		return public, nil, err
	}

	if err = public.SetMarklId(
		markl.FormatIdAgeX25519Pub,
		private.PublicKey().Bytes(),
	); err != nil {
		return public, nil, err
	}

	if sealed, err = seal(secret, recipients); err != nil {
		return public, nil, err
	}

	return public, sealed, nil
}

// Reseal opens a sealed store key and seals the same key to recipients.
// The store key, and so every blob, is unchanged. Removing a recipient
// this way stops that key opening the NEW document only; see FDR 0011
// "Revocation is not rotation".
func Reseal(
	sealed []byte,
	open Opener,
	recipients []markl.Id,
) (resealed []byte, err error) {
	secret, err := openSealed(sealed, open)
	if err != nil {
		return nil, err
	}

	defer zero(secret)

	return seal(secret, recipients)
}

// SealedRecipients returns the encryption recipients a sealed store key
// is sealed to, without opening it.
func SealedRecipients(sealed []byte) ([]markl.Id, error) {
	doc, err := pigpen.ParseDocument(sealed)
	if err != nil {
		return nil, fmt.Errorf("parsing sealed store key: %w", err)
	}

	return doc.EncryptionRecipients(), nil
}

// recipientResolveTimeout bounds loading a pigpen. A pointer document runs
// an external resolver that may make a network call (piggy RFC 0010).
const recipientResolveTimeout = 30 * time.Second

// LoadRecipients reads the pigpen at path and returns its encryption
// recipients. The file may take any piggy-ids form: RFC 0003 lines, a
// pigpen recipient set, or a pointer to a remotely hosted pigpen, which
// is resolved through its `pigpen-resolver-<kind>` binary on PATH.
func LoadRecipients(path string) ([]markl.Id, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pigpen: %w", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		recipientResolveTimeout,
	)
	defer cancel()

	recipients, err := pigpen_resolve.LoadRecipients(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("loading recipients from %s: %w", path, err)
	}

	if len(recipients) == 0 {
		return nil, fmt.Errorf("pigpen %s names no encryption recipients", path)
	}

	return recipients, nil
}

// RecipientSetDigest returns the blake2b256 digest of piggy's canonical
// recipient-set bytes (RFC 0008 §2.3). Two recipient lists have the same
// digest exactly when pigpen.SameRecipientSet says they are the same set,
// so a store can record this at seal time and compare later to detect a
// changed pigpen without opening anything.
func RecipientSetDigest(recipients []markl.Id) (digest markl.Id, err error) {
	hash, repool := markl.FormatHashBlake2b256.GetHash() //repool:owned
	defer repool()

	if _, err = hash.Write(pigpen.CanonicalRecipientSet(recipients)); err != nil {
		return digest, fmt.Errorf("hashing recipient set: %w", err)
	}

	got, repoolGot := hash.GetMarklId() //repool:owned
	defer repoolGot()

	if err = digest.SetDigest(got); err != nil {
		return digest, fmt.Errorf("recipient set digest: %w", err)
	}

	return digest, nil
}

func seal(secret []byte, recipients []markl.Id) ([]byte, error) {
	if len(recipients) == 0 {
		return nil, fmt.Errorf(
			"sealing store key: no encryption recipients; nothing could ever open it",
		)
	}

	doc, err := pigpen.Seal(secret, recipients, nil)
	if err != nil {
		return nil, fmt.Errorf("sealing store key: %w", err)
	}

	sealed, err := doc.MarshalText()
	if err != nil {
		return nil, fmt.Errorf("encoding sealed store key: %w", err)
	}

	return sealed, nil
}

func openSealed(sealed []byte, open Opener) ([]byte, error) {
	doc, err := pigpen.ParseDocument(sealed)
	if err != nil {
		return nil, fmt.Errorf("parsing sealed store key: %w", err)
	}

	secret, err := open(doc)
	if err != nil {
		// %w: an agent failure must stay recognisable to
		// agent.IsErrAgent, so readers do not mistake an unreachable
		// agent for a bad blob.
		return nil, fmt.Errorf("opening sealed store key: %w", err)
	}

	if len(secret) != secretSize {
		zero(secret)

		return nil, fmt.Errorf(
			"sealed store key holds %d bytes, want %d",
			len(secret),
			secretSize,
		)
	}

	return secret, nil
}

// MakeIOWrapper returns the blob IO wrapper for a store whose public key
// is public. Writes encrypt to public and touch neither load nor open.
// The first read calls load and open once; the outcome, success or
// failure, is kept for the life of the wrapper.
func MakeIOWrapper(
	public markl.Id,
	load SealedKeyLoader,
	open Opener,
) (interfaces.IOWrapper, error) {
	writer, err := public.GetIOWrapper()
	if err != nil {
		return nil, fmt.Errorf("store public key: %w", err)
	}

	return &ioWrapper{
		public: public,
		writer: writer,
		load:   load,
		open:   open,
	}, nil
}

type ioWrapper struct {
	public markl.Id
	writer interfaces.IOWrapper
	load   SealedKeyLoader
	open   Opener

	unsealOnce sync.Once
	reader     interfaces.IOWrapper
	unsealErr  error
}

func (wrapper *ioWrapper) WrapWriter(w io.Writer) (io.WriteCloser, error) {
	return wrapper.writer.WrapWriter(w)
}

func (wrapper *ioWrapper) WrapReader(r io.Reader) (io.ReadCloser, error) {
	wrapper.unsealOnce.Do(wrapper.unseal)

	if wrapper.unsealErr != nil {
		return nil, wrapper.unsealErr
	}

	return wrapper.reader.WrapReader(r)
}

func (wrapper *ioWrapper) unseal() {
	sealed, err := wrapper.load()
	if err != nil {
		wrapper.unsealErr = fmt.Errorf("loading sealed store key: %w", err)
		return
	}

	secret, err := openSealed(sealed, wrapper.open)
	if err != nil {
		wrapper.unsealErr = err
		return
	}

	defer zero(secret)

	// Guard against a sidecar that belongs to a different store: its key
	// would open cleanly and then fail every blob with a confusing
	// wrong-recipient error.
	private, err := ecdh.X25519().NewPrivateKey(secret)
	if err != nil {
		wrapper.unsealErr = fmt.Errorf("sealed store key: %w", err)
		return
	}

	if string(private.PublicKey().Bytes()) != string(wrapper.public.GetBytes()) {
		wrapper.unsealErr = fmt.Errorf(
			"sealed store key does not match the store's public key %s",
			wrapper.public,
		)

		return
	}

	var identity markl.Id

	if err = identity.SetMarklId(markl.FormatIdAgeX25519Sec, secret); err != nil {
		wrapper.unsealErr = fmt.Errorf("sealed store key: %w", err)
		return
	}

	if wrapper.reader, err = identity.GetIOWrapper(); err != nil {
		wrapper.unsealErr = fmt.Errorf("sealed store key: %w", err)
	}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
