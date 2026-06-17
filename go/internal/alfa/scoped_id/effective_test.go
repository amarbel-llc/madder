//go:build test

package scoped_id

import "testing"

func TestEffectiveName(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"default", "default"},
		{"shared", "shared"},
		{".archive", "archive"},
		{"//sys", "sys"},
	} {
		var id Id
		if err := id.Set(tc.input); err != nil {
			t.Fatalf("Set(%q): %v", tc.input, err)
		}

		if got := EffectiveName(id); got != tc.want {
			t.Errorf("EffectiveName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}

	// Set("") errors, so an unnamed id is constructed directly; it must
	// resolve to DefaultName.
	if got := EffectiveName(Id{}); got != DefaultName {
		t.Errorf("EffectiveName(zero Id) = %q, want %q", got, DefaultName)
	}
}

func TestEffectiveId(t *testing.T) {
	// Unnamed id: name forced to DefaultName, location preserved.
	eff := EffectiveId(MakeWithLocation("", LocationTypeCwd))
	if eff.GetName() != DefaultName {
		t.Errorf("EffectiveId(unnamed).GetName() = %q, want %q", eff.GetName(), DefaultName)
	}
	if eff.GetLocationType() != LocationTypeCwd {
		t.Errorf("EffectiveId(unnamed) location = %v, want %v", eff.GetLocationType(), LocationTypeCwd)
	}

	// Named id: name and location both preserved.
	named := EffectiveId(MakeWithLocation("foo", LocationTypeXDGUser))
	if named.GetName() != "foo" {
		t.Errorf("EffectiveId(named).GetName() = %q, want %q", named.GetName(), "foo")
	}
	if named.GetLocationType() != LocationTypeXDGUser {
		t.Errorf("EffectiveId(named) location = %v, want %v", named.GetLocationType(), LocationTypeXDGUser)
	}
}
