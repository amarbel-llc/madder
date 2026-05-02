// Package markl_registrations holds madder's standard purpose and
// purpose-id-alias registrations for the markl framework. Each
// registration is exposed as a named exported var so consumers (e.g.
// dodder) can introspect madder's canonical vocabulary, use entries as
// templates, or selectively replay a subset. The aggregate slices
// AllPurposes and AllAliases are the data that init() iterates to
// install everything in markl's registry.
//
// Format registrations stay inside markl proper — those are
// infrastructure, not vocabulary, and they cannot be safely moved out
// without exposing the package-private formats map.
//
// To activate the registrations, blank-import this package from a
// binary's main package:
//
//	import _ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
package markl_registrations

//go:generate dagnabit export

import (
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
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
	PurposeBlobDigestV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeBlobDigestV1,
		Type: markl.PurposeTypeBlobDigest,
		FormatIds: []string{
			markl.FormatIdHashSha256,
			markl.FormatIdHashBlake2b256,
		},
	}

	PurposeObjectDigestV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeObjectDigestV1,
		Type: markl.PurposeTypeObjectDigest,
		FormatIds: []string{
			markl.FormatIdHashSha256,
			markl.FormatIdHashBlake2b256,
		},
	}

	PurposeObjectDigestV2Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeObjectDigestV2,
		Type: markl.PurposeTypeObjectDigest,
		FormatIds: []string{
			markl.FormatIdHashSha256,
			markl.FormatIdHashBlake2b256,
		},
	}

	PurposeV5MetadataDigestWithoutTaiOpts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeV5MetadataDigestWithoutTai,
		Type: markl.PurposeTypeObjectDigest,
		FormatIds: []string{
			markl.FormatIdHashSha256,
			markl.FormatIdHashBlake2b256,
		},
	}

	PurposeObjectMotherSigV1Opts = markl.RegisterPurposeOpts{
		Id:        markl.PurposeObjectMotherSigV1,
		Type:      markl.PurposeTypeObjectMotherSig,
		FormatIds: []string{markl.FormatIdEd25519Sig},
	}

	PurposeObjectMotherSigV2Opts = markl.RegisterPurposeOpts{
		Id:        markl.PurposeObjectMotherSigV2,
		Type:      markl.PurposeTypeObjectMotherSig,
		FormatIds: []string{markl.FormatIdEd25519Sig},
	}

	PurposeObjectSigV0Opts = markl.RegisterPurposeOpts{
		Id:        markl.PurposeObjectSigV0,
		Type:      markl.PurposeTypeObjectSig,
		FormatIds: []string{markl.FormatIdEd25519Sig},
	}

	PurposeObjectSigV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeObjectSigV1,
		Type: markl.PurposeTypeObjectSig,
		FormatIds: []string{
			markl.FormatIdEd25519Sig,
			markl.FormatIdEcdsaP256Sig,
		},
		Related: map[string]string{
			markl.RelatedRoleDigest:    markl.PurposeObjectDigestV1,
			markl.RelatedRoleMotherSig: markl.PurposeObjectMotherSigV1,
		},
	}

	PurposeObjectSigV2Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeObjectSigV2,
		Type: markl.PurposeTypeObjectSig,
		FormatIds: []string{
			markl.FormatIdEd25519Sig,
			markl.FormatIdEcdsaP256Sig,
		},
		Related: map[string]string{
			markl.RelatedRoleDigest:    markl.PurposeObjectDigestV2,
			markl.RelatedRoleMotherSig: markl.PurposeObjectMotherSigV2,
		},
	}

	PurposeRepoPrivateKeyV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeRepoPrivateKeyV1,
		Type: markl.PurposeTypePrivateKey,
		FormatIds: []string{
			markl.FormatIdEd25519Sec,
			markl.FormatIdEd25519SSH,
			markl.FormatIdEcdsaP256SSH,
		},
		Related: map[string]string{
			markl.RelatedRolePublicKey: markl.PurposeRepoPubKeyV1,
		},
	}

	PurposeRepoPubKeyV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeRepoPubKeyV1,
		Type: markl.PurposeTypeRepoPubKey,
		FormatIds: []string{
			markl.FormatIdEd25519Pub,
			markl.FormatIdEcdsaP256Pub,
		},
	}

	PurposeRequestAuthChallengeV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeRequestAuthChallengeV1,
		Type: markl.PurposeTypeRequestAuth,
	}

	PurposeRequestAuthResponseV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeRequestAuthResponseV1,
		Type: markl.PurposeTypeRequestAuth,
	}

	PurposeRequestRepoSigV1Opts = markl.RegisterPurposeOpts{
		Id:   markl.PurposeRequestRepoSigV1,
		Type: markl.PurposeTypeRequestAuth,
		FormatIds: []string{
			markl.FormatIdEd25519Sig,
			markl.FormatIdEcdsaP256Sig,
		},
	}

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
)

// AllPurposes is the canonical, ordered list of purposes madder
// registers. Order is deterministic but consumers must not depend on
// it — registration is order-independent under markl's lazy Related
// validation (ADR 0006).
//
// TODO(#108) codegen this slice from the per-purpose vars so adding
// a new Purpose*Opts entry doesn't require a manual append.
var AllPurposes = []markl.RegisterPurposeOpts{
	PurposeBlobDigestV1Opts,
	PurposeObjectDigestV1Opts,
	PurposeObjectDigestV2Opts,
	PurposeV5MetadataDigestWithoutTaiOpts,
	PurposeObjectMotherSigV1Opts,
	PurposeObjectMotherSigV2Opts,
	PurposeObjectSigV0Opts,
	PurposeObjectSigV1Opts,
	PurposeObjectSigV2Opts,
	PurposeRepoPrivateKeyV1Opts,
	PurposeRepoPubKeyV1Opts,
	PurposeRequestAuthChallengeV1Opts,
	PurposeRequestAuthResponseV1Opts,
	PurposeRequestRepoSigV1Opts,
	PurposeMadderPubKeyV1Opts,
	PurposeMadderPrivateKeyV0Opts,
	PurposeMadderPrivateKeyV1Opts,
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
