package mcp

import (
	"context"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/golf/command"
)

func TestBridgeUnknownCommand(t *testing.T) {
	utility := command.NewUtility("madder", "test")
	bridge := MakeBridge(utility)
	_, err := bridge.RunCommand(
		context.Background(),
		"nonexistent-command",
		nil,
		100_000,
	)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}
