package huh

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/golf/command"
)

func TestHuhPrompterImplementsPrompter(t *testing.T) {
	var _ command.Prompter = &Prompter{}
}
