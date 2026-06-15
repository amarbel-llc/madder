//go:build test

package scoped_id

import (
	"bytes"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/xdg_location_type"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	_ "github.com/amarbel-llc/madder/go/internal/charlie/markl_registrations"
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
		{"//system", xdg_location_type.XDGSystem, "system", 0},
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

// TestId_SystemScopeSpellings pins the FDR-0019 system-scope grammar:
// `//name` is the forced-system spelling (remoteFirst=false) and
// `/name` is remote-first (remoteFirst=true). Both parse to XDGSystem
// and round-trip through String/Canonical to their own spelling. madder
// resolves both to the system scope; the marker exists for dodder.
func TestId_SystemScopeSpellings(t *testing.T) {
	cases := []struct {
		input           string
		wantRemoteFirst bool
	}{
		{"//system", false},
		{"/system", true},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var id Id
			if err := id.Set(tc.input); err != nil {
				t.Fatalf("Set(%q): %v", tc.input, err)
			}

			if id.GetLocationType() != xdg_location_type.XDGSystem {
				t.Errorf("location = %v, want XDGSystem", id.GetLocationType())
			}
			if id.GetName() != "system" {
				t.Errorf("name = %q, want %q", id.GetName(), "system")
			}
			if id.IsRemoteFirst() != tc.wantRemoteFirst {
				t.Errorf("IsRemoteFirst() = %v, want %v",
					id.IsRemoteFirst(), tc.wantRemoteFirst)
			}
			if got := id.String(); got != tc.input {
				t.Errorf("String() = %q, want %q", got, tc.input)
			}
			if got := id.Canonical(); got != tc.input {
				t.Errorf("Canonical() = %q, want %q", got, tc.input)
			}
		})
	}
}

// TestId_Set_AllSlashesRejected: `//` with no name is rejected, mirroring
// the all-dots guard.
func TestId_Set_AllSlashesRejected(t *testing.T) {
	var id Id
	if err := id.Set("//"); err == nil {
		t.Fatalf("Set(\"//\"): want error, got nil")
	}
}

// TestId_Less_RemoteFirstTiebreaker: same name+location, plain system
// (`//name`) sorts before remote-first (`/name`).
func TestId_Less_RemoteFirstTiebreaker(t *testing.T) {
	var plain, remote Id
	if err := plain.Set("//system"); err != nil {
		t.Fatal(err)
	}
	if err := remote.Set("/system"); err != nil {
		t.Fatal(err)
	}

	if !plain.Less(remote) {
		t.Errorf("//system should sort before /system")
	}
	if remote.Less(plain) {
		t.Errorf("/system should not sort before //system")
	}
}

// TestId_Set_NameCharsetEnforced pins the second half of #227:
// blob-store(7) declares the name portion may contain only
// [a-zA-Z0-9_-], but Set never enforced it. A path-shaped value like
// "/home/user/store" parsed as an XDGSystem id whose NAME contained
// slashes, which init then string-joined into a nested directory tree
// under the store root instead of rejecting the argument.
func TestId_Set_NameCharsetEnforced(t *testing.T) {
	invalid := []string{
		"/home/user/store", // the #227 mangle: separators in the name
		"foo/bar",
		".foo/bar",
		"%foo/bar",
		"foo.bar", // dot is only meaningful as a location prefix
		"foo bar",
		"föö",
	}

	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			var id Id
			if err := id.Set(input); err == nil {
				t.Errorf(
					"Set(%q) = nil, want name-charset error (got id %q)",
					input, id.String(),
				)
			}
		})
	}

	valid := []string{
		"default",
		"sftp-default",
		"under_score",
		"..deep",
		"%scratch",
		"_custom",
		"~legacy",
		"/system",
		"//system",
	}

	for _, input := range valid {
		t.Run(input, func(t *testing.T) {
			var id Id
			if err := id.Set(input); err != nil {
				t.Errorf("Set(%q): unexpected error: %v", input, err)
			}
		})
	}
}

// TestId_DisambiguatedString pins #231's rendering contract: the form
// a user can type to address the store unambiguously even when a file
// of the same name exists in CWD. XDG-user ids render with the `~`
// parse-only alias (their bare String() is exactly what file-first
// resolution would re-route to the file); every other location's
// prefixed String() already bypasses the filesystem probe.
func TestId_DisambiguatedString(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"default", "~default"},
		{"~legacy", "~legacy"},
		{".archive", ".archive"},
		{"..deep", "..deep"},
		{"%scratch", "%scratch"},
		{"_custom", "_custom"},
		{"/system", "/system"},
		{"//system", "//system"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var id Id
			if err := id.Set(tc.input); err != nil {
				t.Fatalf("Set(%q): %v", tc.input, err)
			}

			if got := id.DisambiguatedString(); got != tc.want {
				t.Errorf(
					"DisambiguatedString() = %q, want %q",
					got, tc.want,
				)
			}
		})
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

// makeTestDigest constructs a valid blake2b256 markl.Id from a stable
// fixture byte pattern. Returns the Id and its blech32 string form for
// inline embedding into test inputs.
func makeTestDigest(t *testing.T, seed byte) (markl.Id, string) {
	t.Helper()
	bites := make([]byte, 32)
	for i := range bites {
		bites[i] = seed + byte(i)
	}
	var id markl.Id
	if err := id.SetMarklId(markl.FormatIdHashBlake2b256, bites); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}
	return id, id.String()
}

func TestId_Set_ParsesDigestSuffix(t *testing.T) {
	_, digestText := makeTestDigest(t, 0x10)

	cases := []struct {
		input      string
		wantName   string
		wantCwd    bool
		wantDigest string // expected GetMarklFormatId
	}{
		{
			input:      "default@" + digestText,
			wantName:   "default",
			wantDigest: markl.FormatIdHashBlake2b256,
		},
		{
			input:      ".archive@" + digestText,
			wantName:   "archive",
			wantCwd:    true,
			wantDigest: markl.FormatIdHashBlake2b256,
		},
		{
			input:    "default",
			wantName: "default",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.input, func(t *testing.T) {
			var id Id
			if err := id.Set(c.input); err != nil {
				t.Fatalf("Set(%q): %v", c.input, err)
			}
			if id.GetName() != c.wantName {
				t.Errorf("GetName = %q, want %q", id.GetName(), c.wantName)
			}
			gotCwd := id.GetLocationType() == xdg_location_type.Cwd
			if gotCwd != c.wantCwd {
				t.Errorf("Cwd = %v, want %v", gotCwd, c.wantCwd)
			}
			if c.wantDigest == "" {
				if id.HasDigest() {
					t.Errorf("HasDigest = true, want false")
				}
				return
			}
			if !id.HasDigest() {
				t.Fatalf("HasDigest = false, want true")
			}
			gotFmt := id.GetDigest().GetMarklFormat().GetMarklFormatId()
			if gotFmt != c.wantDigest {
				t.Errorf("digest format = %q, want %q", gotFmt, c.wantDigest)
			}
		})
	}
}

func TestId_Canonical_RoundTripsDigest(t *testing.T) {
	_, digestText := makeTestDigest(t, 0x20)
	input := ".archive@" + digestText

	var id Id
	if err := id.Set(input); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := id.Canonical()
	if got != input {
		t.Errorf("Canonical round-trip: got %q, want %q", got, input)
	}
}

// String() MUST NOT include the digest suffix — it is the
// BlobStoreMap key and is used as a sort key in many places.
func TestId_String_OmitsDigest(t *testing.T) {
	_, digestText := makeTestDigest(t, 0x30)
	input := ".archive@" + digestText

	var id Id
	if err := id.Set(input); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got := id.String()
	const want = ".archive"
	if got != want {
		t.Errorf("String() = %q, want %q (bare form, no digest)", got, want)
	}
}

func TestId_Set_RejectsMalformedDigest(t *testing.T) {
	var id Id
	err := id.Set("default@not-a-real-markl-id")
	if err == nil {
		t.Fatal("Set: expected error on malformed digest, got nil")
	}
}

func TestId_MarshalText_RoundTrip(t *testing.T) {
	_, digestText := makeTestDigest(t, 0x40)
	input := "default@" + digestText

	var src Id
	if err := src.Set(input); err != nil {
		t.Fatalf("Set: %v", err)
	}

	bites, err := src.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}

	var dst Id
	if err := dst.UnmarshalText(bites); err != nil {
		t.Fatalf("UnmarshalText: %v", err)
	}

	if dst.Canonical() != src.Canonical() {
		t.Errorf("round-trip: got %q, want %q",
			dst.Canonical(), src.Canonical())
	}
}

func TestId_Less_DigestTieBreaker(t *testing.T) {
	var d1, d2 markl.Id
	bytes1 := make([]byte, 32)
	bytes1[0] = 0x01
	bytes2 := make([]byte, 32)
	bytes2[0] = 0x02

	if err := d1.SetMarklId(markl.FormatIdHashBlake2b256, bytes1); err != nil {
		t.Fatal(err)
	}
	if err := d2.SetMarklId(markl.FormatIdHashBlake2b256, bytes2); err != nil {
		t.Fatal(err)
	}

	a := Make("default").WithDigest(d1)
	b := Make("default").WithDigest(d2)

	if a.Less(b) == b.Less(a) {
		t.Fatal("Less is not antisymmetric for digest-only-differing ids")
	}

	want := bytes.Compare(bytes1, bytes2) < 0
	if a.Less(b) != want {
		t.Errorf("Less direction: a.Less(b) = %v, want %v",
			a.Less(b), want)
	}
}

func TestId_Less_BareIdsUnchanged(t *testing.T) {
	a := Make("alpha")
	b := Make("bravo")
	if !a.Less(b) || b.Less(a) {
		t.Errorf("bare-id ordering regressed: a.Less(b)=%v b.Less(a)=%v",
			a.Less(b), b.Less(a))
	}
}

func TestId_WithDigest_RoundTrip(t *testing.T) {
	var digest markl.Id
	if err := digest.SetMarklId(
		markl.FormatIdHashBlake2b256,
		make([]byte, 32),
	); err != nil {
		t.Fatalf("SetMarklId: %v", err)
	}

	id := Make("default").WithDigest(digest)

	if !id.HasDigest() {
		t.Fatal("HasDigest = false, want true")
	}

	got := id.GetDigest()
	if got.GetMarklFormat().GetMarklFormatId() != markl.FormatIdHashBlake2b256 {
		t.Errorf("digest format = %v, want blake2b256",
			got.GetMarklFormat().GetMarklFormatId())
	}

	if Make("default").HasDigest() {
		t.Error("zero-value digest should report HasDigest = false")
	}
}
