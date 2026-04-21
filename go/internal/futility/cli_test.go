package futility

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// runHandler is a convenience for tests that need to pop args from a Request.
// It wraps the body in req.Context.Run so ContextCancel-style panics surface
// as errors rather than leaking out.
func runHandler(req Request, body func()) error {
	return req.Context.Run(func(_ errors.Context) {
		body()
	})
}

func TestRunCLIDispatchesRun(t *testing.T) {
	var called bool
	var gotName string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "greet",
		Params: []Param{
			StringFlag{Name: "name", Description: "Name to greet", Required: true},
		},
		Run: func(req Request) (*Result, error) {
			called = true
			if err := runHandler(req, func() {
				gotName = req.PopArg("name")
			}); err != nil {
				return nil, err
			}
			return TextResult("hello " + gotName), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"greet", "--name", "world"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if !called {
		t.Error("Run handler was not called")
	}
	if gotName != "world" {
		t.Errorf("name = %q, want %q", gotName, "world")
	}
}

func TestRunCLIBoolFlag(t *testing.T) {
	var got bool
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			BoolFlag{Name: "verbose", Description: "Verbose output"},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("verbose") == "true"
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "--verbose"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if !got {
		t.Error("verbose should be true")
	}
}

func TestRunCLIIntFlag(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			IntFlag{Name: "count", Description: "Count"},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("count")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "--count", "42"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "42" {
		t.Errorf("count = %q, want 42", got)
	}
}

func TestRunCLIUnknownCommand(t *testing.T) {
	app := NewUtility("test", "test app")
	err := app.RunCLI(context.Background(), []string{"nonexistent"}, StubPrompter{})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestRunCLIEqualsFlag(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			StringFlag{Name: "name", Description: "Name"},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("name")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "--name=alice"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "alice" {
		t.Errorf("name = %q, want %q", got, "alice")
	}
}

func TestRunCLIPositionalArg(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "open",
		Params: []Param{
			StringArg{Name: "target", Description: "target path"},
			BoolFlag{Name: "verbose", Description: "verbose output"},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				// Walk params in Command.Params order: verbose flag first
				// (flags come before positional args in the internal input),
				// then target. But positional assignment order walks the
				// declared list — target is declared first here, so it is
				// the first arg.
				got = req.PopArg("target")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"open", "eng/worktrees/repo/branch"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "eng/worktrees/repo/branch" {
		t.Errorf("target = %q, want %q", got, "eng/worktrees/repo/branch")
	}
}

func TestRunCLIPrefixSubcommand(t *testing.T) {
	var called bool
	app := NewUtility("test", "test app")

	sub := NewUtility("perms", "Manage permissions")
	sub.AddCommand(&Command{
		Name: "check",
		Run: func(req Request) (*Result, error) {
			called = true
			return TextResult("ok"), nil
		},
	})
	app.MergeWithPrefix(sub, "perms")

	err := app.RunCLI(context.Background(), []string{"perms", "check"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if !called {
		t.Error("perms-check handler was not called")
	}
}

func TestRunCLIShortBoolFlag(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			BoolFlag{Name: "verbose", Description: "Verbose output", Short: 'v'},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("verbose")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "-v"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "true" {
		t.Errorf("verbose = %q, want true when using -v", got)
	}
}

func TestRunCLIShortStringFlag(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			StringFlag{Name: "name", Description: "Name", Short: 'n'},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("name")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "-n", "alice"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "alice" {
		t.Errorf("name = %q, want %q", got, "alice")
	}
}

func TestRunCLIShortFlagEquals(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			StringFlag{Name: "name", Description: "Name", Short: 'n'},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("name")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "-n=bob"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "bob" {
		t.Errorf("name = %q, want %q", got, "bob")
	}
}

func TestRunCLIShortIntFlag(t *testing.T) {
	var got string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			IntFlag{Name: "count", Description: "Count", Short: 'c'},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				got = req.PopArg("count")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "-c", "7"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if got != "7" {
		t.Errorf("count = %q, want 7", got)
	}
}

func TestRunCLIShortAndLongFlagsMixed(t *testing.T) {
	var name, verbose string
	app := NewUtility("test", "test app")
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			StringFlag{Name: "name", Description: "Name", Short: 'n'},
			BoolFlag{Name: "verbose", Description: "Verbose", Short: 'v'},
		},
		Run: func(req Request) (*Result, error) {
			_ = runHandler(req, func() {
				name = req.PopArg("name")
				verbose = req.PopArg("verbose")
			})
			return TextResult(""), nil
		},
	})

	err := app.RunCLI(context.Background(), []string{"cmd", "-v", "--name", "alice"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI: %v", err)
	}
	if verbose != "true" {
		t.Errorf("verbose = %q, want true", verbose)
	}
	if name != "alice" {
		t.Errorf("name = %q, want %q", name, "alice")
	}
}

func TestDuplicateShortFlagsPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate short flags")
		}
	}()

	app := NewUtility("test", "test app")
	// Panic should fire at AddCommand, not at RunCLI.
	app.AddCommand(&Command{
		Name: "cmd",
		Params: []Param{
			BoolFlag{Name: "verbose", Description: "Verbose", Short: 'v'},
			BoolFlag{Name: "version", Description: "Version", Short: 'v'},
		},
		Run: func(req Request) (*Result, error) {
			return TextResult(""), nil
		},
	})
}

func TestRunCLIHelpFlag(t *testing.T) {
	app := NewUtility("myapp", "My application")
	app.Description.Long = "A longer description of my application."
	app.AddCommand(&Command{
		Name:        "serve",
		Description: Description{Short: "Start the server"},
		Run: func(req Request) (*Result, error) {
			t.Error("serve handler should not be called when -h is passed at app level")
			return nil, nil
		},
	})

	tests := []struct {
		name string
		args []string
	}{
		{"dash h", []string{"-h"}},
		{"double dash help", []string{"--help"}},
		{"help command", []string{"help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.RunCLI(context.Background(), tt.args, StubPrompter{})
			if err != nil {
				t.Errorf("RunCLI(%v): %v", tt.args, err)
			}
		})
	}
}

func TestRunCLICommandHelpFlag(t *testing.T) {
	app := NewUtility("myapp", "My application")
	app.AddCommand(&Command{
		Name: "serve",
		Description: Description{
			Short: "Start the server",
			Long:  "Start the server and listen on stdin/stdout.",
		},
		Params: []Param{
			IntFlag{Name: "port", Description: "Port to listen on", Short: 'p'},
			BoolFlag{Name: "verbose", Description: "Verbose output"},
		},
		Run: func(req Request) (*Result, error) {
			t.Error("serve handler should not be called when -h is passed")
			return nil, nil
		},
	})

	tests := []struct {
		name string
		args []string
	}{
		{"command dash h", []string{"serve", "-h"}},
		{"command double dash help", []string{"serve", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.RunCLI(context.Background(), tt.args, StubPrompter{})
			if err != nil {
				t.Errorf("RunCLI(%v): %v", tt.args, err)
			}
		})
	}
}

func TestRunCLIPrefixSubcommandHelpFlag(t *testing.T) {
	app := NewUtility("myapp", "My application")

	sub := NewUtility("mcp", "MCP server")
	sub.AddCommand(&Command{
		Name:        "stdio",
		Description: Description{Short: "MCP over stdio"},
		Run: func(req Request) (*Result, error) {
			t.Error("mcp stdio handler should not be called when -h is passed")
			return nil, nil
		},
	})
	app.MergeWithPrefix(sub, "mcp")

	tests := []struct {
		name string
		args []string
	}{
		{"prefix command dash h", []string{"mcp", "stdio", "-h"}},
		{"prefix command double dash help", []string{"mcp", "stdio", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := app.RunCLI(context.Background(), tt.args, StubPrompter{})
			if err != nil {
				t.Errorf("RunCLI(%v): %v", tt.args, err)
			}
		})
	}
}

// --- AddCmd + RunCLI integration tests ---
// These tests verify that commands registered via AddCmd (using the new Param
// API) work correctly through RunCLI's flag parsing and positional mapping.

func TestRunCLIWithParamArgPositionalOnly(t *testing.T) {
	app := NewUtility("test", "test app")

	var gotArg string
	app.AddCmd("open", &captureParamCmd{
		params: []Param{
			StringArg{Name: "path", Description: "File path", Required: true},
		},
		onRun: func(req Request) {
			gotArg = req.PopArg("path")
		},
	})

	err := app.RunCLI(t.Context(), []string{"open", "/tmp/foo"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI error: %v", err)
	}
	if gotArg != "/tmp/foo" {
		t.Errorf("PopArg(path) = %q, want %q", gotArg, "/tmp/foo")
	}
}

func TestRunCLIWithParamFlagsBeforeArg(t *testing.T) {
	app := NewUtility("test", "test app")

	var gotArg, gotFlag string
	app.AddCmd("init", &captureParamCmd{
		params: []Param{
			StringFlag{Name: "encryption", Description: "Encryption type"},
			StringArg{Name: "store-id", Description: "Store ID", Required: true},
		},
		onRun: func(req Request) {
			gotFlag = req.PopArg("encryption")
			gotArg = req.PopArg("store-id")
		},
	})

	err := app.RunCLI(t.Context(), []string{"init", "-encryption", "none", ".default"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI error: %v", err)
	}
	if gotFlag != "none" {
		t.Errorf("PopArg(encryption) = %q, want %q", gotFlag, "none")
	}
	if gotArg != ".default" {
		t.Errorf("PopArg(store-id) = %q, want %q", gotArg, ".default")
	}
}

// --- Flags-before-positionals enforcement (issue #39) ---

func TestRunCLIFlagsBeforeArgExactReproduction(t *testing.T) {
	// Exact reproduction from #39: two flags (string + bool=value) before positional.
	app := NewUtility("test", "test app")

	var gotArg, gotEncryption, gotLock string
	app.AddCmd("init", &captureParamCmd{
		params: []Param{
			StringFlag{Name: "encryption", Description: "Encryption type"},
			BoolFlag{Name: "lock-internal-files", Description: "Lock internal files"},
			StringArg{Name: "store-id", Description: "Store ID", Required: true},
		},
		onRun: func(req Request) {
			gotEncryption = req.PopArg("encryption")
			gotLock = req.PopArg("lock-internal-files")
			gotArg = req.PopArg("store-id")
		},
	})

	err := app.RunCLI(t.Context(), []string{"init", "-encryption", "none", "-lock-internal-files=false", ".default"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI error: %v", err)
	}
	if gotEncryption != "none" {
		t.Errorf("encryption = %q, want %q", gotEncryption, "none")
	}
	if gotLock != "false" {
		t.Errorf("lock-internal-files = %q, want %q", gotLock, "false")
	}
	if gotArg != ".default" {
		t.Errorf("store-id = %q, want %q", gotArg, ".default")
	}
}

func TestRunCLIVariadicArg(t *testing.T) {
	app := NewUtility("test", "test app")

	var gotArgs []string
	app.AddCmd("sync", &captureParamCmd{
		params: []Param{
			StringArg{Name: "store-ids", Description: "Store IDs", Variadic: true},
		},
		onRun: func(req Request) {
			gotArgs = req.PopArgs()
		},
	})

	err := app.RunCLI(t.Context(), []string{"sync", ".default", ".sha256", ".backup"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI error: %v", err)
	}
	if len(gotArgs) != 3 {
		t.Fatalf("PopArgs() returned %d args, want 3: %v", len(gotArgs), gotArgs)
	}
	if gotArgs[0] != ".default" || gotArgs[1] != ".sha256" || gotArgs[2] != ".backup" {
		t.Errorf("PopArgs() = %v, want [.default .sha256 .backup]", gotArgs)
	}
}

func TestRunCLIVariadicArgWithFlags(t *testing.T) {
	app := NewUtility("test", "test app")

	var gotVerbose, gotArgs string
	app.AddCmd("sync", &captureParamCmd{
		params: []Param{
			BoolFlag{Name: "verbose", Description: "Verbose", Short: 'v'},
			StringArg{Name: "store-ids", Description: "Store IDs", Variadic: true},
		},
		onRun: func(req Request) {
			gotVerbose = req.PopArg("verbose")
			gotArgs = strings.Join(req.PopArgs(), ",")
		},
	})

	err := app.RunCLI(t.Context(), []string{"sync", "-v", ".default", ".sha256"}, StubPrompter{})
	if err != nil {
		t.Fatalf("RunCLI error: %v", err)
	}
	if gotVerbose != "true" {
		t.Errorf("verbose = %q, want %q", gotVerbose, "true")
	}
	if gotArgs != ".default,.sha256" {
		t.Errorf("store-ids = %q, want %q", gotArgs, ".default,.sha256")
	}
}

func TestShortFlagNotInJSONSchema(t *testing.T) {
	cmd := &Command{
		Name: "status",
		Params: []Param{
			BoolFlag{Name: "verbose", Description: "Verbose output", Short: 'v'},
		},
	}

	schema := cmd.InputSchema()
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	props := parsed["properties"].(map[string]any)
	verboseProp := props["verbose"].(map[string]any)

	if _, exists := verboseProp["short"]; exists {
		t.Error("Short field should not appear in JSON schema")
	}
}
