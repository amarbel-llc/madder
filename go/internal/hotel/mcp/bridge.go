package mcp

import (
	"context"
	"io"
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/golf/command"
)

type BridgeResult struct {
	Stdout    string
	Stderr    string
	Truncated bool
	BytesSeen int
}

type Bridge struct {
	utility *command.Utility
}

func MakeBridge(utility *command.Utility) Bridge {
	return Bridge{
		utility: utility,
	}
}

func (b Bridge) RunCommand(
	ctx context.Context,
	cmdName string,
	cliArgs []string,
	maxBytes int,
) (BridgeResult, error) {
	outWriter := MakeLimitingWriter(maxBytes)
	errWriter := MakeLimitingWriter(maxBytes)

	// Redirect stdout/stderr to capture output
	oldOut, oldErr := os.Stdout, os.Stderr

	outR, outW, err := os.Pipe()
	if err != nil {
		return BridgeResult{}, err
	}

	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return BridgeResult{}, err
	}

	os.Stdout = outW
	os.Stderr = errW

	// Capture output in background
	outDone := make(chan struct{})
	errDone := make(chan struct{})

	go func() {
		io.Copy(outWriter, outR)
		close(outDone)
	}()

	go func() {
		io.Copy(errWriter, errR)
		close(errDone)
	}()

	args := append([]string{cmdName}, cliArgs...)
	runErr := b.utility.RunCLI(ctx, args, command.StubPrompter{})

	outW.Close()
	errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	<-outDone
	<-errDone
	outR.Close()
	errR.Close()

	result := BridgeResult{
		Stdout:    outWriter.String(),
		Stderr:    errWriter.String(),
		Truncated: outWriter.Truncated(),
		BytesSeen: outWriter.BytesSeen(),
	}

	return result, runErr
}
