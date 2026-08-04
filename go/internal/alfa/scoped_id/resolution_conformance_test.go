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

// TestConformance_ResolveFrom_ConfigLocationRelative pins FDR-0010's
// config-location-relative resolution ("The Normative Resolver": an id
// inside a config means what it meant WHERE THE CONFIG LIVES). A
// reference parsed from a config discovered `configDepth` CWD-levels up
// resolves as if the resolver `cd`'d to that config's directory first:
// only CWD-scoped ids are rebased (by adding configDepth to their own
// dot-depth); user/system/cache ids are scope-absolute and unchanged.
// This is the primitive that makes an ancestor multi's `.member` resolve
// to the ancestor's store, not the process cwd's — the mechanism whose
// absence was the cross-scope multi-resolution bug (dodder#196/#359
// class).
func TestConformance_ResolveFrom_ConfigLocationRelative(t *testing.T) {
	cases := []struct {
		spelling    string
		configDepth uint
		want        string // String() after rebasing
	}{
		// CWD scope IS rebased: the config's depth adds to the ref's.
		{".widgets", 0, ".widgets"},    // config at cwd: identity
		{".widgets", 1, "..widgets"},   // config one ancestor up
		{".widgets", 2, "...widgets"},  // config two ancestors up
		{"..widgets", 1, "...widgets"}, // a `..` ref one level up
		{"...gadgets", 0, "...gadgets"},

		// Fixed scopes are scope-absolute: never rebased, whatever the
		// config's depth.
		{"widgets", 2, "widgets"},     // XDG user
		{"~widgets", 2, "widgets"},    // XDG user (parse-only alias)
		{"//widgets", 2, "//widgets"}, // XDG system (forced)
		{"/widgets", 2, "/widgets"},   // XDG system (remote-first)
		{"%gadgets", 2, "%gadgets"},   // XDG cache
	}

	for _, tc := range cases {
		t.Run(tc.spelling, func(t *testing.T) {
			var id Id
			if err := id.Set(tc.spelling); err != nil {
				t.Fatalf("Set(%q): %v", tc.spelling, err)
			}
			got := id.ResolveFrom(tc.configDepth).String()
			if got != tc.want {
				t.Errorf("ResolveFrom(%d): got %q, want %q",
					tc.configDepth, got, tc.want)
			}
		})
	}
}

// TestConformance_ResolveFrom_PreservesDigest pins that rebasing a
// digest-pinned CWD reference (the form a multi config stores for its
// members) carries the pin through unchanged — the rebased id addresses a
// different physical store but still asserts the SAME pinned instance.
func TestConformance_ResolveFrom_PreservesDigest(t *testing.T) {
	const digest = "blake2b256-9ft3m74l5t2ppwjrvfg3wp380jqj2zfrm6zevxqx34sdethvey0s5vm9gd"

	var id Id
	if err := id.Set(".widgets@" + digest); err != nil {
		t.Fatalf("Set: %v", err)
	}

	rebased := id.ResolveFrom(1)
	if !rebased.HasDigest() {
		t.Fatal("digest dropped by ResolveFrom")
	}
	if got, want := rebased.Canonical(), ".widgets@"+digest; got != want {
		t.Errorf("Canonical after rebase: got %q, want %q", got, want)
	}
	if got := rebased.String(); got != "..widgets" {
		t.Errorf("String after rebase: got %q, want %q", got, "..widgets")
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
