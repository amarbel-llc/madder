package output_format

import (
	"os"
	"testing"

	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/files"
)

func TestResolveFor(t *testing.T) {
	cases := []struct {
		name     string
		format   Format
		isTTY    bool
		piped    Format
		expected Format
	}{
		{"auto tty", FormatAuto, true, FormatCRAP, FormatTAP},
		{"auto piped crap default", FormatAuto, false, FormatCRAP, FormatCRAP},
		{"auto piped ndjson default", FormatAuto, false, FormatNDJSON, FormatNDJSON},
		{"explicit ndjson wins on tty", FormatNDJSON, true, FormatCRAP, FormatNDJSON},
		{"explicit crap wins piped", FormatCRAP, false, FormatNDJSON, FormatCRAP},
		{"explicit tap wins piped", FormatTAP, false, FormatCRAP, FormatTAP},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if actual := c.format.resolveFor(c.isTTY, c.piped); actual != c.expected {
				t.Errorf("expected %q, got %q", c.expected, actual)
			}
		})
	}
}

func TestIsTTYPipeIsNotATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	// Test-pipe teardown; both close errors are non-actionable here.
	defer files.CloseReadOnly(r)
	defer files.CloseReadOnly(w)

	if IsTTY(w) {
		t.Error("IsTTY(pipe write end) = true, want false")
	}
	if IsTTY(r) {
		t.Error("IsTTY(pipe read end) = true, want false")
	}
}

func TestSetAcceptsCrap(t *testing.T) {
	var f Format
	if err := f.Set("crap"); err != nil {
		t.Fatalf("Set(crap): %v", err)
	}
	if f != FormatCRAP {
		t.Errorf("expected %q, got %q", FormatCRAP, f)
	}
}
