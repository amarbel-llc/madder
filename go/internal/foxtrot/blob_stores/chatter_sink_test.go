//go:build test

package blob_stores

import (
	"bytes"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
	"code.linenisgreat.com/madder/go/internal/charlie/fd"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
)

// envDirWithErrSink doubles as an env that carries an env_ui-style err
// sink, the shape env_local.Env and blob_store_env.BlobStoreEnv present
// when they pass themselves to MakeBlobStore. The embedded env_dir.Env
// is nil — only the methods MakeBlobStore touches on the SFTP
// construction path are implemented.
type envDirWithErrSink struct {
	env_dir.Env
	errStd fd.Std
}

func (env envDirWithErrSink) GetErr() fd.Std { return env.errStd }

func (env envDirWithErrSink) GetActiveContext() interfaces.ActiveContext {
	return &spyActiveContext{}
}

func (env envDirWithErrSink) GetBlobWriteObserver() domain_interfaces.BlobWriteObserver {
	return nil
}

// envDirBare is an env WITHOUT a GetErr sink — the minimal env_dir.Env
// shape. MakeBlobStore must fall back to the process-global stderr
// printer for it, not require the capability.
type envDirBare struct {
	env_dir.Env
}

func (env envDirBare) GetActiveContext() interfaces.ActiveContext {
	return &spyActiveContext{}
}

func (env envDirBare) GetBlobWriteObserver() domain_interfaces.BlobWriteObserver {
	return nil
}

// makeSftpConfigNamed builds a ConfigNamed for an explicit SFTP config.
// SFTP store construction is lazy (no dial until first use), so
// MakeBlobStore on this config exercises printer wiring without any
// network dependency.
func makeSftpConfigNamed(t *testing.T) blob_store_configs.ConfigNamed {
	t.Helper()

	var id scoped_id.Id
	if err := id.Set("sftp-chatter"); err != nil {
		t.Fatalf("scoped_id.Set: %v", err)
	}

	return blob_store_configs.ConfigNamed{
		Path: directory_layout.MakeBlobStorePath(id, "/base", "/config"),
		Config: blob_store_configs.TypedConfig{
			Blob: &blob_store_configs.TomlSFTPV0{
				Host:       "sftp.invalid",
				User:       "nobody",
				RemotePath: "/remote",
			},
		},
	}
}

// TestMakeBlobStore_ChatterRoutesToEnvErrSink pins #228: blob-store
// chatter (and the ssh-helper chatter that shares the same printer)
// must route through the env's err sink — which honors env_ui's
// CustomErr / UIFileIsStderr options — rather than the process-global
// ui.Err() stderr printer. Consumers like cutting-garden redirect that
// sink to keep lazy SFTP dial/host-key/remote-config lines from
// corrupting a live terminal UI.
func TestMakeBlobStore_ChatterRoutesToEnvErrSink(t *testing.T) {
	var buf bytes.Buffer
	env := envDirWithErrSink{errStd: fd.MakeStdFromWriter(&buf)}

	store, err := MakeBlobStore(env, makeSftpConfigNamed(t), nil)
	if err != nil {
		t.Fatalf("MakeBlobStore: %v", err)
	}

	sftpStore, ok := store.(*remoteSftp)
	if !ok {
		t.Fatalf("expected *remoteSftp, got %T", store)
	}

	// The store's printer is what initialize()/sshDial print through;
	// emitting one probe line through it stands in for the lazy-init
	// chatter without needing a live SSH endpoint.
	sftpStore.uiPrinter.Printf("chatter probe")

	out := buf.String()
	if !strings.Contains(out, "chatter probe") {
		t.Fatalf(
			"store chatter did not reach the env err sink; buffer: %q",
			out,
		)
	}
	if !strings.Contains(out, "sftp-chatter") {
		t.Errorf(
			"chatter missing the blob_store id prefix; buffer: %q",
			out,
		)
	}
}

// TestMakeBlobStore_FallsBackToGlobalErrWithoutSink guards the default:
// an env without a GetErr capability must still construct cleanly,
// landing chatter on the process-global stderr printer exactly as
// before #228.
func TestMakeBlobStore_FallsBackToGlobalErrWithoutSink(t *testing.T) {
	store, err := MakeBlobStore(envDirBare{}, makeSftpConfigNamed(t), nil)
	if err != nil {
		t.Fatalf("MakeBlobStore: %v", err)
	}

	if _, ok := store.(*remoteSftp); !ok {
		t.Fatalf("expected *remoteSftp, got %T", store)
	}
}
