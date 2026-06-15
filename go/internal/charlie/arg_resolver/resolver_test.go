//go:build test

package arg_resolver

import (
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
)

// TestFormatShadowWarning_HintIsActionable pins #231: the
// disambiguation hint must name a form that actually resolves to the
// store. For an unprefixed (XDG user) id the bare name is exactly the
// argument that just resolved to the file (ModeFile precedes
// ModeStoreSwitch), so the hint must use the `~`-prefixed parse-only
// alias instead. Prefixed ids keep their own rendering — their string
// form never collides with a bare filename probe.
func TestFormatShadowWarning_HintIsActionable(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		wantHint string
	}{
		{
			name:     "xdg-user ids hint the ~ alias",
			id:       "shadowed",
			wantHint: `or "~shadowed" for the blob-store-id`,
		},
		{
			name:     "cwd ids keep the dot form",
			id:       ".shadowed",
			wantHint: `or ".shadowed" for the blob-store-id`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var id scoped_id.Id
			if err := id.Set(tc.id); err != nil {
				t.Fatalf("Set(%q): %v", tc.id, err)
			}

			warning := FormatShadowWarning("shadowed", id)

			if !strings.Contains(warning, tc.wantHint) {
				t.Errorf(
					"warning %q missing actionable hint %q",
					warning, tc.wantHint,
				)
			}
		})
	}
}
