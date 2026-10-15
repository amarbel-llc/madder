//go:build test

package blob_stores

import (
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// eofOnClose is a client write end whose Close reports io.EOF, as the
// transport of a connection that has already dropped does. It also ends the
// other direction of the connection, so the client's receive loop stops and
// Close can return.
type eofOnClose struct {
	io.WriteCloser
	alsoClose io.Closer
}

func (w eofOnClose) Close() error {
	_ = w.WriteCloser.Close()
	_ = w.alsoClose.Close()

	return io.EOF
}

func newSftpClientWhoseCloseReportsEOF(t *testing.T) *sftp.Client {
	t.Helper()

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	server, err := sftp.NewServer(pipeRWC{r: c2sR, w: s2cW})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	go func() { _ = server.Serve() }()

	client, err := sftp.NewClientPipe(s2cR, eofOnClose{c2sW, s2cW})
	if err != nil {
		t.Fatalf("NewClientPipe: %v", err)
	}

	return client
}

// Tearing down a store whose connection has dropped must not panic and must
// not report an error: the connection being gone is what teardown wants.
// The error wrapper panics on io.EOF, which is what madder#306 hit at exit
// after a long sync lost its connection.
func TestSftpClose_ToleratesADroppedConnection(t *testing.T) {
	store := &remoteSftp{sftpClient: newSftpClientWhoseCloseReportsEOF(t)}

	done := make(chan any, 1)

	go func() {
		done <- recoverPanic(func() {
			if err := store.close(); err != nil {
				t.Errorf("close returned %v, want nil for a dropped connection", err)
			}
		})
	}()

	select {
	case recovered := <-done:
		if recovered != nil {
			t.Fatalf("close panicked: %v", recovered)
		}

	case <-time.After(10 * time.Second):
		t.Fatalf("close did not return")
	}
}

func TestIsSftpConnectionGone(t *testing.T) {
	gone := map[string]error{
		"io.EOF":                io.EOF,
		"wrapped io.EOF":        fmt.Errorf("closing: %w", io.EOF),
		"unexpected EOF":        io.ErrUnexpectedEOF,
		"closed network":        net.ErrClosed,
		"closed file":           os.ErrClosed,
		"sftp connection lost":  sftp.ErrSSHFxConnectionLost,
		"sftp no connection":    sftp.ErrSSHFxNoConnection,
		"wrapped sftp lost":     fmt.Errorf("stat: %w", sftp.ErrSSHFxConnectionLost),
		"os error wrapping EOF": &os.PathError{Op: "close", Path: "x", Err: io.EOF},
	}

	for name, err := range gone {
		if !isSftpConnectionGone(err) {
			t.Errorf("%s: not recognised as a dropped connection", name)
		}
	}

	notGone := map[string]error{
		"nil":               nil,
		"no such file":      os.ErrNotExist,
		"permission":        os.ErrPermission,
		"sftp failure":      sftp.ErrSSHFxFailure,
		"sftp no such file": sftp.ErrSSHFxNoSuchFile,
		"plain":             stderrors.New("boom"),
	}

	for name, err := range notGone {
		if isSftpConnectionGone(err) {
			t.Errorf("%s: wrongly recognised as a dropped connection", name)
		}
	}
}
