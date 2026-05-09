//go:build test

package blob_store_id

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/xdg_location_type"
)

func TestId_Set_String_RoundTrip(t *testing.T) {
	cases := []struct {
		input        string
		wantLocation xdg_location_type.Typee
		wantName     string
		wantDepth    uint
	}{
		{"default", xdg_location_type.XDGUser, "default", 0},
		{".default", xdg_location_type.Cwd, "default", 0},
		{"..default", xdg_location_type.Cwd, "default", 1},
		{"...rsync_dot_net", xdg_location_type.Cwd, "rsync_dot_net", 2},
		{"/system", xdg_location_type.XDGSystem, "system", 0},
		{"%scratch", xdg_location_type.XDGCache, "scratch", 0},
		{"_custom", xdg_location_type.Unknown, "custom", 0},
		{"~legacy", xdg_location_type.XDGUser, "legacy", 0},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var id Id
			if err := id.Set(tc.input); err != nil {
				t.Fatalf("Set(%q): %v", tc.input, err)
			}

			if id.location != tc.wantLocation {
				t.Errorf("location = %v, want %v", id.location, tc.wantLocation)
			}
			if id.id != tc.wantName {
				t.Errorf("name = %q, want %q", id.id, tc.wantName)
			}
			if id.cwdDepth != tc.wantDepth {
				t.Errorf("cwdDepth = %d, want %d", id.cwdDepth, tc.wantDepth)
			}

			// `~legacy` is the documented one-way alias: parse to
			// XDGUser, render without prefix.
			wantString := tc.input
			if tc.input == "~legacy" {
				wantString = "legacy"
			}

			if got := id.String(); got != wantString {
				t.Errorf("String() = %q, want %q", got, wantString)
			}
		})
	}
}

func TestId_Set_AllDotsRejected(t *testing.T) {
	var id Id
	if err := id.Set("..."); err == nil {
		t.Fatalf("Set(\"...\"): want error, got nil")
	}
}

func TestId_Canonical_DropsDepth(t *testing.T) {
	var id Id
	if err := id.Set("...rsync_dot_net"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got, want := id.Canonical(), ".rsync_dot_net"; got != want {
		t.Errorf("Canonical() = %q, want %q", got, want)
	}

	if got, want := id.String(), "...rsync_dot_net"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestId_MarshalText_AlwaysCanonical(t *testing.T) {
	var id Id
	if err := id.Set("..default"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	bs, err := id.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}

	if got, want := string(bs), ".default"; got != want {
		t.Errorf("MarshalText() = %q, want %q (canonical, no extra dots)", got, want)
	}
}

func TestId_WithCwdDepth(t *testing.T) {
	id := MakeWithLocation("default", LocationTypeCwd)
	if got, want := id.String(), ".default"; got != want {
		t.Errorf("zero-depth String() = %q, want %q", got, want)
	}

	deeper := id.WithCwdDepth(2)
	if got, want := deeper.String(), "...default"; got != want {
		t.Errorf("WithCwdDepth(2).String() = %q, want %q", got, want)
	}

	// Original unchanged (value semantics).
	if got, want := id.String(), ".default"; got != want {
		t.Errorf("original mutated: String() = %q, want %q", got, want)
	}
}

func TestId_Less_DepthAsTiebreaker(t *testing.T) {
	mk := func(depth uint) Id {
		return MakeWithLocation("default", LocationTypeCwd).WithCwdDepth(depth)
	}

	deepest := mk(0)
	next := mk(1)

	if !deepest.Less(next) {
		t.Errorf("deepest (depth=0) should sort before next (depth=1)")
	}
	if next.Less(deepest) {
		t.Errorf("next (depth=1) should not sort before deepest (depth=0)")
	}

	xdgUser := MakeWithLocation("default", LocationTypeXDGUser)
	if !deepest.Less(xdgUser) {
		t.Errorf("Cwd should sort before XDGUser regardless of depth")
	}
}
