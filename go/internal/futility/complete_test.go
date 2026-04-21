package futility

import (
	"context"
	"testing"
)

func TestCompleteIsHidden(t *testing.T) {
	app := NewUtility("myapp", "My app")

	for name, cmd := range app.AllCommands() {
		if name == "__complete" {
			if !cmd.Hidden {
				t.Error("__complete command should be hidden")
			}
			return
		}
	}
	t.Error("__complete command not found")
}

func TestCompleteUnknownCommandReturnsNoError(t *testing.T) {
	app := NewUtility("myapp", "My app")

	err := app.RunCLI(context.Background(), []string{
		"__complete", "--command", "nonexistent", "--param", "foo",
	}, StubPrompter{})
	if err != nil {
		t.Fatalf("expected no error for unknown command, got: %v", err)
	}
}

func TestCompleteParamWithoutCompleterReturnsNoError(t *testing.T) {
	app := NewUtility("myapp", "My app")
	app.AddCommand(&Command{
		Name:        "status",
		Description: Description{Short: "Show status"},
		Params: []Param{
			StringFlag{Name: "repo_path", Description: "Path to repo"},
		},
	})

	err := app.RunCLI(context.Background(), []string{
		"__complete", "--command", "status", "--param", "repo_path",
	}, StubPrompter{})
	if err != nil {
		t.Fatalf("expected no error for param without completer, got: %v", err)
	}
}
