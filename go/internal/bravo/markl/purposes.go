package markl

import (
	"fmt"
)

// purposes currently treated as formats
const (
	// TODO move to ids' builtin types
	// and then add registration
	// keep sorted

	// Blob Digests
	PurposeBlobDigestV1 = "dodder-blob-digest-sha256-v1"

	// Object Digests
	PurposeObjectDigestV1             = "dodder-object-digest-sha256-v1"
	PurposeObjectDigestV2             = "dodder-object-digest-v2"
	PurposeV5MetadataDigestWithoutTai = "dodder-object-metadata-digest-without_tai-v1"

	// Object Mother Sigs
	PurposeObjectMotherSigV1 = "dodder-object-mother-sig-v1"
	PurposeObjectMotherSigV2 = "dodder-object-mother-sig-v2"

	// Object Sigs
	PurposeObjectSigV0 = "dodder-repo-sig-v1"
	PurposeObjectSigV1 = "dodder-object-sig-v1"
	PurposeObjectSigV2 = "dodder-object-sig-v2"

	// Request Auth
	PurposeRequestAuthResponseV1  = "dodder-request_auth-response-v1"
	PurposeRequestRepoSigV1       = "dodder-request_auth-repo-sig-v1"
	PurposeRequestAuthChallengeV1 = "dodder-request_auth-challenge-v1"

	// PubKeys
	PurposeRepoPubKeyV1   = "dodder-repo-public_key-v1"
	PurposeMadderPubKeyV1 = "madder-public_key-v1"

	// PrivateKeys
	PurposeRepoPrivateKeyV1   = "dodder-repo-private_key-v1"
	PurposeMadderPrivateKeyV0 = "madder-private_key-v0"
	PurposeMadderPrivateKeyV1 = "madder-private_key-v1"
)

func init() {
	// purposes that need to be reregistered
	makePurpose(
		PurposeBlobDigestV1,
		PurposeTypeBlobDigest,
		FormatIdHashSha256,
		FormatIdHashBlake2b256,
	)

	makePurpose(
		PurposeObjectDigestV1,
		PurposeTypeObjectDigest,
		FormatIdHashSha256,
		FormatIdHashBlake2b256,
	)

	makePurpose(
		PurposeObjectDigestV2,
		PurposeTypeObjectDigest,
		FormatIdHashSha256,
		FormatIdHashBlake2b256,
	)

	makePurpose(
		PurposeV5MetadataDigestWithoutTai,
		PurposeTypeObjectDigest,
		FormatIdHashSha256,
		FormatIdHashBlake2b256,
	)

	makePurpose(
		PurposeObjectMotherSigV1,
		PurposeTypeObjectMotherSig,
		FormatIdEd25519Sig,
	)

	makePurpose(
		PurposeObjectMotherSigV2,
		PurposeTypeObjectMotherSig,
		FormatIdEd25519Sig,
	)

	makePurpose(
		PurposeObjectSigV0,
		PurposeTypeObjectSig,
		FormatIdEd25519Sig,
	)

	RegisterPurpose(RegisterPurposeOpts{
		Id:   PurposeObjectSigV1,
		Type: PurposeTypeObjectSig,
		FormatIds: []string{
			FormatIdEd25519Sig,
			FormatIdEcdsaP256Sig,
		},
		Related: map[string]string{
			RelatedRoleDigest:    PurposeObjectDigestV1,
			RelatedRoleMotherSig: PurposeObjectMotherSigV1,
		},
	})

	RegisterPurpose(RegisterPurposeOpts{
		Id:   PurposeObjectSigV2,
		Type: PurposeTypeObjectSig,
		FormatIds: []string{
			FormatIdEd25519Sig,
			FormatIdEcdsaP256Sig,
		},
		Related: map[string]string{
			RelatedRoleDigest:    PurposeObjectDigestV2,
			RelatedRoleMotherSig: PurposeObjectMotherSigV2,
		},
	})

	makePurpose(
		PurposeRepoPrivateKeyV1,
		PurposeTypePrivateKey,
		FormatIdEd25519Sec,
		FormatIdEd25519SSH,
		FormatIdEcdsaP256SSH,
	)

	makePurpose(
		PurposeRepoPubKeyV1,
		PurposeTypeRepoPubKey,
		FormatIdEd25519Pub,
		FormatIdEcdsaP256Pub,
	)

	makePurpose(PurposeRequestAuthChallengeV1, PurposeTypeRequestAuth)
	makePurpose(PurposeRequestAuthResponseV1, PurposeTypeRequestAuth)

	makePurpose(
		PurposeRequestRepoSigV1,
		PurposeTypeRequestAuth,
		FormatIdEd25519Sig,
		FormatIdEcdsaP256Sig,
	)

	makePurpose(
		PurposeMadderPubKeyV1,
		PurposeTypePubKey,
		FormatIdEd25519Pub,
		FormatIdEcdsaP256Pub,
	)

	makePurpose(
		PurposeMadderPrivateKeyV0,
		PurposeTypePrivateKey,
		FormatIdEd25519Sec,
		FormatIdAgeX25519Sec,
	)

	makePurpose(
		PurposeMadderPrivateKeyV1,
		PurposeTypePrivateKey,
		FormatIdEd25519Sec,
		FormatIdAgeX25519Sec,
		FormatIdPivyEcdhP256Pub,
	)
}

var purposes = map[string]Purpose{}

type Purpose struct {
	id        string
	tipe      PurposeType
	formatIds map[string]struct{}
	related   map[string]string
}

func GetPurpose(purposeId string) Purpose {
	purpose, ok := purposes[purposeId]

	if !ok {
		panic(fmt.Sprintf("no purpose registered for id %q", purposeId))
	}

	return purpose
}

// RegisterPurposeOpts is the public registration shape for purposes.
//
// Related is a free-form role → purposeId map (see ADR 0006). Values are
// validated lazily: lookups via Purpose.GetRelated succeed for any registered
// role, and a downstream caller passing the result to GetPurpose is what
// surfaces typos.
type RegisterPurposeOpts struct {
	Id        string
	Type      PurposeType
	FormatIds []string
	Related   map[string]string
}

// RegisterPurpose installs a Purpose in the package-global registry. Panics
// if Id is already registered, or if FormatIds contains a duplicate. Returns
// the constructed Purpose so callers may keep a typed handle.
func RegisterPurpose(opts RegisterPurposeOpts) Purpose {
	if _, alreadyExists := purposes[opts.Id]; alreadyExists {
		panic(fmt.Sprintf("purpose already registered: %q", opts.Id))
	}

	purpose := Purpose{
		id:        opts.Id,
		tipe:      opts.Type,
		formatIds: make(map[string]struct{}, len(opts.FormatIds)),
		related:   make(map[string]string, len(opts.Related)),
	}

	for _, formatId := range opts.FormatIds {
		if _, ok := purpose.formatIds[formatId]; ok {
			panic(
				fmt.Sprintf("format id (%q) registered for purpose (%q) more than once",
					formatId,
					opts.Id,
				),
			)
		}

		purpose.formatIds[formatId] = struct{}{}
	}

	for role, relatedId := range opts.Related {
		purpose.related[role] = relatedId
	}

	purposes[opts.Id] = purpose
	return purpose
}

// makePurpose is the legacy variadic shim retained so existing init() blocks
// in this package compile unchanged. New registrations should call
// RegisterPurpose directly.
func makePurpose(purposeId string, purposeType PurposeType, formatIds ...string) {
	RegisterPurpose(RegisterPurposeOpts{
		Id:        purposeId,
		Type:      purposeType,
		FormatIds: formatIds,
	})
}

func (purpose Purpose) GetPurposeType() PurposeType {
	return purpose.tipe
}

// GetRelated looks up a related purposeId by role. Returns ("", false) if
// no purpose was registered under that role for this Purpose. The returned
// purposeId is not validated against the registry — pass it to GetPurpose
// to resolve.
func (purpose Purpose) GetRelated(role string) (string, bool) {
	relatedId, ok := purpose.related[role]
	return relatedId, ok
}

// RelatedRoleDigest and RelatedRoleMotherSig are the role names used by
// madder's own sig purposes. Other consumers may define their own role
// constants — markl itself stays role-agnostic per ADR 0006.
const (
	RelatedRoleDigest    = "digest"
	RelatedRoleMotherSig = "mother_sig"
)

func GetDigestTypeForSigType(sigId string) string {
	sig := GetPurpose(sigId)

	digestId, ok := sig.GetRelated(RelatedRoleDigest)
	if !ok {
		panic(fmt.Sprintf("unsupported sig purpose: %q", sigId))
	}

	return digestId
}

func GetMotherSigTypeForSigType(sigId string) string {
	sig := GetPurpose(sigId)

	motherSigId, ok := sig.GetRelated(RelatedRoleMotherSig)
	if !ok {
		panic(fmt.Sprintf("unsupported sig purpose: %q", sigId))
	}

	return motherSigId
}
