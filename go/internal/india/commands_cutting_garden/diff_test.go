package commands_cutting_garden

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// rendererWithProfile builds a lipgloss.Renderer pinned to the given
// profile, sidestepping stdout-based auto-detection so the tests are
// deterministic across TTY/CI environments.
func rendererWithProfile(p termenv.Profile) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(nil)
	r.SetColorProfile(p)
	return r
}

func TestRenderDiffLineKnownMarkers(t *testing.T) {
	r := rendererWithProfile(termenv.ANSI)

	cases := []struct {
		name      string
		line      string
		wantColor string // SGR foreground digits we expect to find
	}{
		{name: "A line", line: "A  path\tfile", wantColor: "32"},
		{name: "D line", line: "D  path\tfile", wantColor: "31"},
		{name: "M line", line: "M  path\tblob a -> b", wantColor: "33"},
		{name: "T line", line: "T  path\tfile -> symlink", wantColor: "35"},
		{name: "B line", line: "B  path\tblob x missing", wantColor: "91"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderDiffLine(r, tc.line)
			if !strings.Contains(got, tc.line) {
				t.Errorf("rendered %q does not contain raw line %q", got, tc.line)
			}
			if !strings.Contains(got, "\x1b[") {
				t.Errorf("rendered %q has no SGR escape", got)
			}
			if !strings.Contains(got, tc.wantColor) {
				t.Errorf("rendered %q does not contain expected color code %q", got, tc.wantColor)
			}
		})
	}
}

func TestRenderDiffLinePassesThroughUnknownMarker(t *testing.T) {
	r := rendererWithProfile(termenv.ANSI)
	for _, line := range []string{"", "X  path", "  not-a-marker"} {
		got := renderDiffLine(r, line)
		if got != line {
			t.Errorf("renderDiffLine(%q) = %q, want unchanged", line, got)
		}
	}
}

func TestRenderDiffLineAsciiProfileDropsCodes(t *testing.T) {
	// A renderer pinned to Ascii should emit no SGR escapes — the
	// "never" path of -color.
	r := rendererWithProfile(termenv.Ascii)
	line := "A  path\tfile"
	got := renderDiffLine(r, line)
	if got != line {
		t.Errorf("Ascii profile leaked SGR: got %q want %q", got, line)
	}
}

func TestNewDiffRendererInvalid(t *testing.T) {
	if _, err := newDiffRenderer("rainbow", nil); err == nil {
		t.Fatalf("expected error for invalid mode")
	}
}

func TestNewDiffRendererAlwaysAndNever(t *testing.T) {
	t.Setenv("NO_COLOR", "1") // must be ignored by always/never

	always, err := newDiffRenderer("always", nil)
	if err != nil {
		t.Fatalf("always: unexpected error %v", err)
	}
	if always.ColorProfile() == termenv.Ascii {
		t.Errorf("always: profile is Ascii, want a colored profile")
	}

	never, err := newDiffRenderer("never", nil)
	if err != nil {
		t.Fatalf("never: unexpected error %v", err)
	}
	if never.ColorProfile() != termenv.Ascii {
		t.Errorf("never: profile %v, want Ascii", never.ColorProfile())
	}
}
