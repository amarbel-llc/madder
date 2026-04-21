package futility

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAll(t *testing.T) {
	app := NewUtility("grit", "Git operations")

	app.AddCommand(&Command{
		Name:        "status",
		Description: Description{Short: "Show status"},
		Params: []Param{
			StringFlag{Name: "repo_path", Description: "Path to repo", Required: true},
		},
	})

	dir := t.TempDir()
	if err := app.GenerateAll(dir); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}

	expected := []string{
		filepath.Join("share", "man", "man1", "grit.1"),
		filepath.Join("share", "man", "man1", "grit-status.1"),
		filepath.Join("share", "bash-completion", "completions", "grit"),
		filepath.Join("share", "zsh", "site-functions", "_grit"),
		filepath.Join("share", "fish", "vendor_completions.d", "grit.fish"),
	}

	for _, rel := range expected {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file missing: %s", rel)
		}
	}
}
