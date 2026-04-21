package huh

import (
	"testing"

	"github.com/amarbel-llc/madder/go/internal/futility"
)

func TestHuhPrompterImplementsPrompter(t *testing.T) {
	var _ futility.Prompter = &Prompter{}
}
