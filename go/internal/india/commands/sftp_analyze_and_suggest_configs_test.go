package commands

import (
	"strings"
	"testing"
)

func TestSftpAnalyzeRegistered(t *testing.T) {
	cmd, ok := utility.GetCommand("sftp-analyze-and-suggest-configs")
	if !ok {
		t.Fatal("sftp-analyze-and-suggest-configs not registered")
	}
	if !strings.Contains(cmd.Description.Short, "analyze") {
		t.Errorf("short desc lacks 'analyze': %q", cmd.Description.Short)
	}
}
