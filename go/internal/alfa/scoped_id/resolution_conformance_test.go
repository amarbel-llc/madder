//go:build test

package scoped_id

// Conformance suite for FDR-0010 (Scoped-ID Resolution),
// docs/features/0010-scoped-id-resolution.md.
//
// PASS anchor. FDR-0010 "Problem Statement" and "Ambiguity rules": the
// GRAMMAR is sound and unchanged — every id carries its scope in its
// prefix, so scope is determined syntactically and cross-scope
// ambiguity is structurally impossible. The defect FDR-0010 fixes is
// RESOLUTION plurality, not the grammar. This test pins that the
// prefix->scope mapping is unambiguous for non-`default` names, so a
// reviewer can see the resolver never has to guess a scope.

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/xdg_location_type"
)

// TestConformance_ScopeIsDeterminedByPrefix pins the grammar->scope
// mapping. Non-`default` names throughout (widgets/gadgets), per
// FDR-0010's conformance requirement.
func TestConformance_ScopeIsDeterminedByPrefix(t *testing.T) {
	cases := []struct {
		spelling    string
		location    xdg_location_type.Type
		cwdDepth    uint
		remoteFirst bool
		name        string
	}{
		{"widgets", xdg_location_type.XDGUser, 0, false, "widgets"},
		{"~widgets", xdg_location_type.XDGUser, 0, false, "widgets"},
		{".widgets", xdg_location_type.Cwd, 0, false, "widgets"},
		{"..widgets", xdg_location_type.Cwd, 1, false, "widgets"},
		{"...gadgets", xdg_location_type.Cwd, 2, false, "gadgets"},
		{"//widgets", xdg_location_type.XDGSystem, 0, false, "widgets"},
		{"/widgets", xdg_location_type.XDGSystem, 0, true, "widgets"},
		{"%gadgets", xdg_location_type.XDGCache, 0, false, "gadgets"},
	}

	for _, tc := range cases {
		t.Run(tc.spelling, func(t *testing.T) {
			var id Id
			if err := id.Set(tc.spelling); err != nil {
				t.Fatalf("Set(%q): %v", tc.spelling, err)
			}
			if id.GetLocationType() != tc.location {
				t.Errorf("scope: got %v, want %v", id.GetLocationType(), tc.location)
			}
			if id.GetCwdDepth() != tc.cwdDepth {
				t.Errorf("cwdDepth: got %d, want %d", id.GetCwdDepth(), tc.cwdDepth)
			}
			if id.IsRemoteFirst() != tc.remoteFirst {
				t.Errorf("remoteFirst: got %v, want %v", id.IsRemoteFirst(), tc.remoteFirst)
			}
			if id.GetName() != tc.name {
				t.Errorf("name: got %q, want %q", id.GetName(), tc.name)
			}
		})
	}
}

// TestConformance_CrossScopeIdsAreDistinct pins that the same bare name
// under three different prefixes parses to three distinct ids naming
// three distinct scopes — the resolver never conflates them, so a bare
// `widgets` (XDG user) can never be shadowed by a CWD-ancestor
// `.widgets`. This is the invariant whose violation was madder#227
// (fixed) and remains dodder#359 (open). PASS anchor.
func TestConformance_CrossScopeIdsAreDistinct(t *testing.T) {
	var user, cwd, system Id
	if err := user.Set("widgets"); err != nil {
		t.Fatal(err)
	}
	if err := cwd.Set(".widgets"); err != nil {
		t.Fatal(err)
	}
	if err := system.Set("//widgets"); err != nil {
		t.Fatal(err)
	}

	if user.GetLocationType() == cwd.GetLocationType() {
		t.Error("bare `widgets` and `.widgets` must name different scopes")
	}
	if cwd.GetLocationType() == system.GetLocationType() {
		t.Error("`.widgets` and `//widgets` must name different scopes")
	}
	if user.GetLocationType() == system.GetLocationType() {
		t.Error("`widgets` and `//widgets` must name different scopes")
	}
}
