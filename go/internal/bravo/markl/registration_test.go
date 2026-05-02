package markl

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
)

// These tests exercise the public registration API added in step 1 of #106
// (see ADR 0006). The registry is package-global, so every test uses unique
// `test-step1-...` ids to avoid colliding with the package's own init().

func TestRegisterPurpose_StoresAndReturnsPurpose(t *testing.T) {
	const id = "test-step1-register-purpose-basic"

	got := RegisterPurpose(RegisterPurposeOpts{
		Id:        id,
		Type:      PurposeTypeObjectSig,
		FormatIds: []string{FormatIdEd25519Sig},
	})

	if got.id != id {
		t.Fatalf("returned Purpose.id: got %q, want %q", got.id, id)
	}

	stored := GetPurpose(id)
	if stored.id != id {
		t.Fatalf("GetPurpose.id: got %q, want %q", stored.id, id)
	}

	if got.GetPurposeType() != PurposeTypeObjectSig {
		t.Fatalf("returned Purpose type: got %v, want PurposeTypeObjectSig", got.GetPurposeType())
	}
}

func TestRegisterPurpose_RelatedRoundTrip(t *testing.T) {
	const (
		id        = "test-step1-related-roundtrip"
		digestId  = "test-step1-related-digest-target"
		motherId  = "test-step1-related-mother-target"
		extraRole = "custom"
		extraId   = "test-step1-related-extra-target"
	)

	purpose := RegisterPurpose(RegisterPurposeOpts{
		Id:   id,
		Type: PurposeTypeObjectSig,
		Related: map[string]string{
			RelatedRoleDigest:    digestId,
			RelatedRoleMotherSig: motherId,
			extraRole:            extraId,
		},
	})

	cases := []struct {
		role string
		want string
	}{
		{RelatedRoleDigest, digestId},
		{RelatedRoleMotherSig, motherId},
		{extraRole, extraId},
	}

	for _, c := range cases {
		got, ok := purpose.GetRelated(c.role)
		if !ok {
			t.Errorf("GetRelated(%q): ok=false, want true", c.role)
			continue
		}
		if got != c.want {
			t.Errorf("GetRelated(%q): got %q, want %q", c.role, got, c.want)
		}
	}

	if got, ok := purpose.GetRelated("never-registered"); ok || got != "" {
		t.Errorf(`GetRelated("never-registered"): got (%q, %v), want ("", false)`, got, ok)
	}
}

func TestRegisterPurpose_LazyValidation_AcceptsUnknownRelatedTarget(t *testing.T) {
	// Per ADR 0006 sub-decision A: Related values are not validated at
	// registration time. Registering a Related target that names a
	// nonexistent purpose must succeed; only a downstream GetPurpose call
	// would surface the typo.
	const (
		id      = "test-step1-lazy-validation"
		bogusId = "test-step1-this-purpose-does-not-exist"
	)

	purpose := RegisterPurpose(RegisterPurposeOpts{
		Id:      id,
		Type:    PurposeTypeObjectSig,
		Related: map[string]string{RelatedRoleDigest: bogusId},
	})

	got, ok := purpose.GetRelated(RelatedRoleDigest)
	if !ok || got != bogusId {
		t.Fatalf("GetRelated: got (%q, %v), want (%q, true)", got, ok, bogusId)
	}
}

func TestRegisterPurpose_PanicsOnDuplicate(t *testing.T) {
	const id = "test-step1-duplicate-purpose"

	RegisterPurpose(RegisterPurposeOpts{Id: id, Type: PurposeTypeObjectSig})

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate purpose registration")
		}
	}()

	RegisterPurpose(RegisterPurposeOpts{Id: id, Type: PurposeTypeObjectSig})
}

func TestRegisterPurpose_PanicsOnDuplicateFormatId(t *testing.T) {
	const id = "test-step1-duplicate-format-id"

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate FormatIds entry")
		}
	}()

	RegisterPurpose(RegisterPurposeOpts{
		Id:        id,
		Type:      PurposeTypeObjectSig,
		FormatIds: []string{FormatIdEd25519Sig, FormatIdEd25519Sig},
	})
}

func TestRegisterFormat_StoresAndReturnsFormat(t *testing.T) {
	const id = "test-step1-register-format-basic"

	in := Format{Id: id, Size: 32}

	got := RegisterFormat(in)

	if got.GetMarklFormatId() != id {
		t.Fatalf("returned Format id: got %q, want %q", got.GetMarklFormatId(), id)
	}

	resolved, err := GetFormatOrError(id)
	if err != nil {
		t.Fatalf("GetFormatOrError(%q): %v", id, err)
	}
	if resolved.GetMarklFormatId() != id {
		t.Fatalf("resolved format id: got %q, want %q", resolved.GetMarklFormatId(), id)
	}
}

func TestRegisterFormat_PanicsOnNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on nil format")
		}
	}()

	RegisterFormat(nil)
}

func TestRegisterFormat_PanicsOnEmptyId(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on empty format id")
		}
	}()

	RegisterFormat(Format{Id: "", Size: 1})
}

func TestRegisterFormat_PanicsOnDuplicate(t *testing.T) {
	const id = "test-step1-duplicate-format"

	RegisterFormat(Format{Id: id, Size: 1})

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate format registration")
		}
	}()

	RegisterFormat(Format{Id: id, Size: 1})
}

func TestRegisterPurposeIdAlias_ResolvesViaGetFormatOrError(t *testing.T) {
	const (
		formatId = "test-step1-alias-target-format"
		aliasId  = "test-step1-purposeid-alias-source"
	)

	target := RegisterFormat(Format{Id: formatId, Size: 8})

	RegisterPurposeIdAlias(aliasId, formatId)

	resolved, err := GetFormatOrError(aliasId)
	if err != nil {
		t.Fatalf("GetFormatOrError(%q): %v", aliasId, err)
	}

	if resolved.GetMarklFormatId() != target.GetMarklFormatId() {
		t.Fatalf("resolved format id: got %q, want %q",
			resolved.GetMarklFormatId(), target.GetMarklFormatId())
	}
}

func TestRegisterPurposeIdAlias_PanicsOnDuplicate(t *testing.T) {
	const aliasId = "test-step1-alias-duplicate"

	RegisterPurposeIdAlias(aliasId, FormatIdEd25519Sec)

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate alias registration")
		}
	}()

	RegisterPurposeIdAlias(aliasId, FormatIdEd25519Sec)
}

// Compile-time check that Format satisfies MarklFormat — protects the
// RegisterFormat tests above from a refactor that breaks the constraint.
var _ domain_interfaces.MarklFormat = Format{}
