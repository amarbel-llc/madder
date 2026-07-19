package huh

import (
	"testing"

	"code.linenisgreat.com/madder/go/internal/futility"
)

func TestHuhPrompterImplementsPrompter(t *testing.T) {
	var _ futility.Prompter = &Prompter{}
}
