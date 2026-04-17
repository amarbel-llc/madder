package commands

import (
	"testing"
)

func TestUtilityHasCommands(t *testing.T) {
	count := 0
	for range utility.AllCommands() {
		count++
	}
	if count == 0 {
		t.Fatal("expected commands to be registered")
	}
}
