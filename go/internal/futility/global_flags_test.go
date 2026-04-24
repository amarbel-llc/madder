package futility

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/flags"
)

// testGlobals is a synthetic utility-side globals struct used by the tests
// in this file. Real utilities (madder, madder-cache) will define their
// own equivalents in their commands package.
type testGlobals struct {
	Verbose bool
}

// makeGlobalFlagsUtility constructs a Utility whose globals back a
// local *testGlobals. Returns the utility and a pointer to the globals
// struct so tests can assert parsed values.
func makeGlobalFlagsUtility(t *testing.T) (*Utility, *testGlobals) {
	t.Helper()

	globals := &testGlobals{}

	u := NewUtility("tool", "test utility with globals")
	u.GlobalParams = []Param{
		BoolFlag{Name: "verbose", Description: "Enable verbose output"},
	}
	u.GlobalFlags = globals
	u.GlobalFlagDefiner = func(fs *flags.FlagSet) {
		fs.BoolVar(&globals.Verbose, "verbose", false, "Enable verbose output")
	}

	return u, globals
}

// TestGlobalFlagsPreSubcommand asserts that a global flag written before
// the subcommand name (`tool --verbose subcmd`) is parsed into the
// utility's globals struct.
func TestGlobalFlagsPreSubcommand(t *testing.T) {
	u, globals := makeGlobalFlagsUtility(t)

	var dispatched bool
	u.AddCommand(&Command{
		Name: "subcmd",
		Run: func(req Request) (*Result, error) {
			dispatched = true
			return TextResult(""), nil
		},
	})

	err := u.RunCLI(
		context.Background(),
		[]string{"--verbose", "subcmd"},
		StubPrompter{},
	)
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if !dispatched {
		t.Error("subcmd was not dispatched")
	}
	if !globals.Verbose {
		t.Error("globals.Verbose = false, want true (pre-subcommand global flag)")
	}
}

// TestGlobalFlagsPostSubcommand asserts that a global flag written after
// the subcommand name (`tool subcmd --verbose`) is parsed into the same
// utility globals struct.
func TestGlobalFlagsPostSubcommand(t *testing.T) {
	u, globals := makeGlobalFlagsUtility(t)

	var dispatched bool
	u.AddCommand(&Command{
		Name: "subcmd",
		Run: func(req Request) (*Result, error) {
			dispatched = true
			return TextResult(""), nil
		},
	})

	err := u.RunCLI(
		context.Background(),
		[]string{"subcmd", "--verbose"},
		StubPrompter{},
	)
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if !dispatched {
		t.Error("subcmd was not dispatched")
	}
	if !globals.Verbose {
		t.Error("globals.Verbose = false, want true (post-subcommand global flag)")
	}
}

// TestGlobalFlagsShortCollisionPanics asserts that registering a Command
// whose Params include a Short rune already claimed by a GlobalParam
// panics at AddCommand time — resolving the TODO at app.go:54-55.
func TestGlobalFlagsShortCollisionPanics(t *testing.T) {
	u := NewUtility("tool", "test")
	u.GlobalParams = []Param{
		BoolFlag{Name: "verbose", Short: 'v', Description: "global verbose"},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error(
				"expected AddCommand to panic on short-flag collision between" +
					" cmd Params and Utility.GlobalParams",
			)
		}
	}()

	u.AddCommand(&Command{
		Name: "subcmd",
		Params: []Param{
			BoolFlag{Name: "version", Short: 'v', Description: "cmd version"},
		},
	})
}

// TestGlobalFlagsRenderInManpage asserts that a declared GlobalParam
// appears in the generated per-command manpage's OPTIONS section (or a
// dedicated GLOBAL OPTIONS section) so users reading `man tool-subcmd`
// can discover the global flag.
func TestGlobalFlagsRenderInManpage(t *testing.T) {
	u, _ := makeGlobalFlagsUtility(t)

	u.AddCommand(&Command{
		Name:        "subcmd",
		Description: Description{Short: "the subcmd"},
	})

	dir := t.TempDir()
	if err := u.GenerateManpages(dir); err != nil {
		t.Fatalf("GenerateManpages: %v", err)
	}

	bites, err := os.ReadFile(filepath.Join(dir, "share", "man", "man1", "tool-subcmd.1"))
	if err != nil {
		t.Fatalf("read tool-subcmd.1: %v", err)
	}

	content := string(bites)
	if !strings.Contains(content, "--verbose") {
		t.Errorf("subcommand manpage missing global --verbose flag\n%s", content)
	}
}

// TestGlobalFlagsRenderInCompletion asserts that a declared GlobalParam
// appears in the generated bash completion so a user tab-completing on
// `tool subcmd --<TAB>` sees the global flag alongside per-cmd flags.
func TestGlobalFlagsRenderInCompletion(t *testing.T) {
	u, _ := makeGlobalFlagsUtility(t)

	u.AddCommand(&Command{
		Name:        "subcmd",
		Description: Description{Short: "the subcmd"},
	})

	dir := t.TempDir()
	if err := u.GenerateCompletions(dir); err != nil {
		t.Fatalf("GenerateCompletions: %v", err)
	}

	bites, err := os.ReadFile(filepath.Join(dir, "share", "bash-completion", "completions", "tool"))
	if err != nil {
		t.Fatalf("read bash completion: %v", err)
	}

	if !strings.Contains(string(bites), "--verbose") {
		t.Errorf("bash completion missing global --verbose flag\n%s", string(bites))
	}
}
