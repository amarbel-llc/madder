// Package main is the test-only fixture binary that mints the recipients
// of a sealed-key store fixture (FDR 0011) without a card: from a 32-byte
// seed it derives an X25519 recipient, and from a fixed scalar a P-256
// recipient, and prints them as a piggy-ids file. The Rust crate's
// conformance tests (crates/madder-store) open the Go-written fixture
// store with the X25519 secret, which is the seed itself, and must parse
// but can never open the P-256 line. See
// docs/plans/2026-10-25-rust-sealed-key-store-design.md.
//
// Not shipped to end users.
package main

import (
	"crypto/ecdh"
	"crypto/elliptic"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"

	_ "code.linenisgreat.com/madder/go/internal/charlie/markl_registrations"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

func main() {
	if err := runMain(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// defaultP256Scalar is a fixed, valid P-256 private scalar (its high byte
// keeps it far below the group order). The fixture only needs the public
// point; nothing ever opens with it.
const defaultP256Scalar = "0110111213141516171819101a1b1c1d1e1f202122232425262728292a2b2c2d"

// runMain parses argv-style arguments and dispatches to run. Split out so
// unit tests can exercise the parsing + minting path without re-execing
// the binary.
func runMain(argv []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("madder-test-mint-recipient", flag.ContinueOnError)
	var (
		seedHex     = fs.String("seed", "", "32-byte hex X25519 secret scalar (required)")
		p256Hex     = fs.String("p256-scalar", defaultP256Scalar, "32-byte hex P-256 private scalar for the second recipient line")
		printSecret = fs.Bool("print-secret", false, "print the X25519 secret as hex instead of the piggy-ids file")
	)
	if err := fs.Parse(argv); err != nil {
		return err
	}

	if *seedHex == "" {
		return fmt.Errorf("must pass -seed <hex>")
	}

	return run(*seedHex, *p256Hex, *printSecret, stdout)
}

// run prints either the X25519 secret (hex) or a two-line piggy-ids file:
// the X25519 recipient derived from seed, then the P-256 recipient derived
// from p256Scalar.
func run(seedHex, p256Hex string, printSecret bool, stdout io.Writer) error {
	seed, err := decodeScalar("seed", seedHex)
	if err != nil {
		return err
	}

	if printSecret {
		_, err = fmt.Fprintf(stdout, "%x\n", seed)
		return err
	}

	x25519, err := x25519RecipientFromSeed(seed)
	if err != nil {
		return err
	}

	scalar, err := decodeScalar("p256-scalar", p256Hex)
	if err != nil {
		return err
	}

	p256, err := p256RecipientFromScalar(scalar)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "%s\n%s\n", x25519, p256)

	return err
}

func decodeScalar(name, value string) ([]byte, error) {
	scalar, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("-%s is not hex: %w", name, err)
	}

	if len(scalar) != 32 {
		return nil, fmt.Errorf("-%s must be 32 bytes, got %d", name, len(scalar))
	}

	return scalar, nil
}

// x25519RecipientFromSeed treats seed as the raw X25519 scalar (clamped at
// use, RFC 7748 §5, so x25519-dalek's StaticSecret::from(seed) derives the
// same public key) and returns the piggy-recipient-v1@age_x25519_pub id.
func x25519RecipientFromSeed(seed []byte) (string, error) {
	private, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		return "", fmt.Errorf("x25519 secret: %w", err)
	}

	return recipientId(markl.FormatIdAgeX25519Pub, private.PublicKey().Bytes())
}

// p256RecipientFromScalar returns the piggy-recipient-v1@pivy_ecdh_p256_pub
// id of the compressed public point of scalar.
func p256RecipientFromScalar(scalar []byte) (string, error) {
	private, err := ecdh.P256().NewPrivateKey(scalar)
	if err != nil {
		return "", fmt.Errorf("p256 scalar: %w", err)
	}

	x, y := elliptic.Unmarshal(elliptic.P256(), private.PublicKey().Bytes())
	if x == nil {
		return "", fmt.Errorf("p256 public key did not unmarshal")
	}

	return recipientId(
		markl.FormatIdPivyEcdhP256Pub,
		elliptic.MarshalCompressed(elliptic.P256(), x, y),
	)
}

func recipientId(formatId string, key []byte) (string, error) {
	var id markl.Id

	if err := id.SetPurposeId(markl.PurposePiggyRecipientV1); err != nil {
		return "", err
	}

	if err := id.SetMarklId(formatId, key); err != nil {
		return "", err
	}

	// StringWithFormat is the wire form: it carries the purpose, which
	// String() drops.
	return id.StringWithFormat(), nil
}
