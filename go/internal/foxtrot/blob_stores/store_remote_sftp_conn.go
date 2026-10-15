package blob_stores

import (
	stderrors "errors"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

// sftpReconnectDelays is how long to wait before each reconnect attempt
// after a connection drops: the first attempt is immediate, then these. When
// they are used up the store is declared unavailable and stays that way, so
// a sync against a remote that is really gone stops within about a minute
// and a half instead of failing every remaining blob separately (madder#306).
var sftpReconnectDelays = []time.Duration{
	time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	30 * time.Second,
}

// dialSftp opens an ssh connection with initializer and an sftp client on
// it.
//
// Pipelined reads and writes are enabled, because without them the client
// issues one MaxPacket-sized request per file at a time, which caps
// throughput at ~MaxPacket/RTT regardless of available bandwidth. The
// single *sftp.Client is goroutine-safe; concurrent requests multiplex over
// the one SSH channel. MaxPacket stays at the default (32 KB), which keeps
// compatibility with SFTP servers that cap packet size to the protocol
// minimum.
func dialSftp(
	initializer func() (*ssh.Client, error),
) (sshClient *ssh.Client, sftpClient *sftp.Client, err error) {
	if sshClient, err = initializer(); err != nil {
		return nil, nil, errors.Wrap(err)
	}

	if sftpClient, err = sftp.NewClient(
		sshClient,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
	); err != nil {
		_ = sshClient.Close()
		return nil, nil, errors.Wrapf(err, "failed to create SFTP client")
	}

	return sshClient, sftpClient, nil
}

// dial makes a connection the way this store was built to: through
// connector when a test or caller supplied one, else from the ssh client
// initializer.
func (blobStore *remoteSftp) dial() (*ssh.Client, *sftp.Client, error) {
	if blobStore.connector != nil {
		return blobStore.connector()
	}

	return dialSftp(blobStore.sshClientInitializer)
}

// client returns the connection requests should use right now. A reconnect
// replaces it, so do not hold the result across operations.
func (blobStore *remoteSftp) client() *sftp.Client {
	blobStore.connLock.RLock()
	defer blobStore.connLock.RUnlock()

	return blobStore.sftpClient
}

// withClient runs op on the current connection. If op fails because the
// connection is gone, the store reconnects (with backoff, see
// sftpReconnectDelays) and op runs once more on the new connection. Use it
// for a request that can safely be sent twice; a request that has already
// had an effect and cannot be repeated should not be wrapped.
func withClient[T any](
	blobStore *remoteSftp,
	op func(client *sftp.Client) (T, error),
) (result T, err error) {
	client := blobStore.client()

	if result, err = op(client); !isSftpConnectionGone(err) {
		return result, err
	}

	if reconnectErr := blobStore.reconnect(client); reconnectErr != nil {
		return result, reconnectErr
	}

	result, err = op(blobStore.client())

	return result, asConnectionLost(err)
}

// asConnectionLost turns the bare io.EOF an ssh channel returns when it
// closes under a request into the sftp connection-lost error. A bare EOF
// makes dewey's errors.Wrap panic (see isSftpConnectionGone) and says
// nothing; the callers wrap what withClient returns.
func asConnectionLost(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return sftp.ErrSSHFxConnectionLost
	}

	return err
}

// withClientErr is withClient for an op that returns only an error.
func (blobStore *remoteSftp) withClientErr(
	op func(client *sftp.Client) error,
) error {
	_, err := withClient(blobStore, func(client *sftp.Client) (struct{}, error) {
		return struct{}{}, op(client)
	})

	return err
}

// removeTemp deletes a temp file left by a write that did not finish. It is
// cleanup after a failure that has already been reported, so a file that is
// already gone, or a connection that is gone with it, is not worth a second
// error: the temp file is then left for the remote's own housekeeping, and
// its name begins with tmp_ so no listing mistakes it for a blob.
func (blobStore *remoteSftp) removeTemp(tempPath string) error {
	err := blobStore.withClientErr(func(client *sftp.Client) error {
		return client.Remove(tempPath)
	})

	if err == nil ||
		stderrors.Is(err, os.ErrNotExist) ||
		isSftpConnectionGone(err) ||
		blob_io.IsBlobStoreUnavailable(err) {
		return nil
	}

	return err
}

// reconnect replaces failed, the client a request just found dead, with a
// fresh connection. Only one goroutine dials; the others wait on the lock,
// find the client already replaced, and carry on. If every attempt fails
// the store is marked unavailable for good and every later call returns that
// error at once.
func (blobStore *remoteSftp) reconnect(failed *sftp.Client) error {
	blobStore.connLock.Lock()
	defer blobStore.connLock.Unlock()

	if blobStore.connectErr != nil {
		return blobStore.connectErr
	}

	if blobStore.sftpClient != failed {
		return nil
	}

	unavailable := func(cause error) error {
		blobStore.connectErr = blob_io.ErrBlobStoreUnavailable{
			StoreId: blobStore.id.String(),
			Reason:  "sftp connection lost and could not be re-established",
			Cause:   cause,
		}

		return blobStore.connectErr
	}

	if blobStore.connector == nil && blobStore.sshClientInitializer == nil {
		return unavailable(errors.Errorf("this store cannot reconnect"))
	}

	logf := func(format string, args ...any) {
		if blobStore.uiPrinter != nil {
			blobStore.uiPrinter.Printf(format, args...)
		}
	}

	var cancelled <-chan struct{}

	if blobStore.ctx != nil {
		cancelled = blobStore.ctx.Done()
	}

	logf("sftp connection lost; reconnecting...")

	var lastErr error

	delays := append([]time.Duration{0}, sftpReconnectDelays...)

	for attempt, delay := range delays {
		if delay > 0 {
			select {
			case <-time.After(delay):

			case <-cancelled:
				return unavailable(errors.Errorf("cancelled while reconnecting"))
			}
		}

		sshClient, sftpClient, err := blobStore.dial()
		if err != nil {
			lastErr = err

			logf(
				"reconnect attempt %d of %d failed: %s",
				attempt+1,
				len(delays),
				err,
			)

			continue
		}

		oldSftp, oldSsh := blobStore.sftpClient, blobStore.sshClient

		blobStore.sshClient, blobStore.sftpClient = sshClient, sftpClient

		// The old connection is dead; closing it releases what it holds and
		// its errors say only that it is dead.
		if oldSftp != nil {
			_ = oldSftp.Close()
		}

		if oldSsh != nil {
			_ = oldSsh.Close()
		}

		blobStore.startKeepalive()

		logf("sftp connection re-established")

		return nil
	}

	return unavailable(lastErr)
}
