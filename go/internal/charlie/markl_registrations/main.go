// Package markl_registrations holds madder's own purpose and
// purpose-id-alias registrations for the markl framework. Each
// registration is exposed as a named exported var so consumers can
// introspect madder's canonical vocabulary or use entries as
// templates. The aggregate slices AllPurposes and AllAliases are the
// data that init() iterates to install everything in markl's registry.
//
// Ownership (madder#255): format registrations and the piggy-*
// purposes live upstream in piggy's go/markl module (piggy#183); the
// blank import below activates them. The dodder-* purposes are
// registered by dodder itself. This package registers only madder-*
// purposes, papi-doc-sig-v1 (until papi has a registration site), and
// the legacy purpose-id aliases madder needs to read its own
// pre-rename on-disk blob stores — dodder gets those aliases by
// importing this package; it must not register its own copies
// (RegisterPurposeIdAlias panics on duplicates).
//
// To activate the registrations, blank-import this package from a
// binary's main package:
//
//	import _ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
package markl_registrations

//go:generate dagnabit export

import (
	// The age and agent blank imports swap the real age/pivy-backed
	// implementations over the core's erroring stubs at their inits
	// (idempotent), restoring the always-on age + pivy recipients the
	// pre-cutover core registered directly. The SSH signing formats stay
	// stubs here — connecting a signer is a consumer-side call (see
	// agent.RegisterSSHEd25519Format).
	_ "github.com/amarbel-llc/piggy/go/markl/age"
	_ "github.com/amarbel-llc/piggy/go/markl/agent"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
	_ "github.com/amarbel-llc/piggy/go/markl/pkgs/markl_registrations"
)

// PurposeIdAlias names a purposeId-shaped string that
// markl.GetFormatOrError should resolve as if it were the named
// formatId. See markl.RegisterPurposeIdAlias for the registration call.
type PurposeIdAlias struct {
	PurposeId string
	FormatId  string
}

// Canonical purpose registrations madder installs in markl's registry
// on init. Each var is the data-form input to markl.RegisterPurpose;
// init() iterates AllPurposes and calls RegisterPurpose for each.
// Downstream consumers may read these vars to introspect madder's
// canonical registrations or use them as templates.

var (
	PurposeBlobStoreConfigDigestV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeBlobStoreConfigDigestV1,
		Type: markl.PurposeTypeBlobDigest,
		FormatIds: []string{
			markl.FormatIdHashBlake2b256,
		},
	}

	// The dodder-* purposes (object digest/sig/mother-sig v1-v3, repo
	// keys, request-auth, blob digest, v5 metadata digest) are no longer
	// registered here: dodder registers its own against the framework
	// (madder#255 steps 2-3). Madder mints none of them.

	PurposeMadderPubKeyV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeMadderPubKeyV1,
		Type: markl.PurposeTypePubKey,
		FormatIds: []string{
			markl.FormatIdEd25519Pub,
			markl.FormatIdEcdsaP256Pub,
		},
	}

	PurposeMadderPrivateKeyV0Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeMadderPrivateKeyV0,
		Type: markl.PurposeTypePrivateKey,
		FormatIds: []string{
			markl.FormatIdEd25519Sec,
			markl.FormatIdAgeX25519Sec,
		},
	}

	PurposeMadderPrivateKeyV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeMadderPrivateKeyV1,
		Type: markl.PurposeTypePrivateKey,
		FormatIds: []string{
			markl.FormatIdEd25519Sec,
			markl.FormatIdAgeX25519Sec,
			markl.FormatIdPivyEcdhP256Pub,
		},
	}

	// The piggy-* purposes are no longer registered here: piggy's own
	// go/markl module registers them in its init (piggy#183 ownership
	// inversion) — see the blank import of piggy's markl_registrations
	// above. Registering them here too would panic on duplicate.

	// Papi document signature (jointly owned with amarbel-llc/papi;
	// mirrored in the piggy-markl crate for the producer side). A slot-9A
	// ecdsa-sha2-nistp256 SSH signature over a PAPI document's JCS bytes,
	// carried as the 64-byte r‖s ecdsa_p256_sig payload after SSH-wire
	// framing is stripped. Registered here transitionally — papi has no
	// Go registration site yet and piggy's module registers only piggy-*
	// purposes; it moves to its owner per madder#255's ownership model.
	// Spans only ecdsa_p256_sig — PAPI's slot-9A YubiKey co-sign world is
	// all P-256; widening to ed25519_sig later is backward-compatible
	// (existing IDs still validate), so start narrow and amend if a
	// software signer appears.
	PurposePapiDocSigV1Opts = markl.RegisterPurposeOpts{
		Id:        markl.PurposePapiDocSigV1,
		Type:      markl.PurposeTypePapiSig,
		FormatIds: []string{markl.FormatIdEcdsaP256Sig},
	}
)

// AllPurposes is the canonical, ordered list of purposes madder
// registers. Order is deterministic but consumers must not depend on
// it — registration is order-independent under markl's lazy Related
// validation (ADR 0006).
//
// TODO(#108) codegen this slice from the per-purpose vars so adding
// a new Purpose*Opts entry doesn't require a manual append.
var AllPurposes = []markl.RegisterPurposeOpts{
	PurposeBlobStoreConfigDigestV1Opts,
	PurposeMadderPubKeyV1Opts,
	PurposeMadderPrivateKeyV0Opts,
	PurposeMadderPrivateKeyV1Opts,
	PurposePapiDocSigV1Opts,
}

// Canonical purpose-id → format-id aliases. See AllPurposes for the
// equivalent purpose-side list.

var (
	AliasZitRepoPrivateKeyV1 = PurposeIdAlias{
		PurposeId: "zit-repo-private_key-v1",
		FormatId:  markl.FormatIdEd25519Sec,
	}

	AliasDodderRepoPrivateKeyV1 = PurposeIdAlias{
		PurposeId: "dodder-repo-private_key-v1",
		FormatId:  markl.FormatIdEd25519Sec,
	}
)

// AllAliases is the canonical, ordered list of purpose-id → format-id
// aliases madder registers. Order is deterministic but consumers must
// not depend on it.
//
// TODO(#108) codegen this slice from the per-alias vars (same shape
// as AllPurposes).
var AllAliases = []PurposeIdAlias{
	AliasZitRepoPrivateKeyV1,
	AliasDodderRepoPrivateKeyV1,
}

func init() {
	for _, opts := range AllPurposes {
		markl.RegisterPurpose(opts)
	}

	for _, alias := range AllAliases {
		markl.RegisterPurposeIdAlias(alias.PurposeId, alias.FormatId)
	}
}
