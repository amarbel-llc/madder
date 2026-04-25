package commands

import (
	"fmt"
	"io"
	"os"
	"path"

	tap "github.com/amarbel-llc/bob/packages/tap-dancer/go"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func init() {
	utility.AddCmd("init", &Init{
		tipe: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
		blobStoreConfig: &blob_store_configs.DefaultType{
			HashTypeId:      blob_store_configs.HashTypeDefault,
			HashBuckets:     blob_store_configs.DefaultHashBuckets,
			CompressionType: compression_type.CompressionTypeDefault,
		},
		desc: futility.Description{
			Short: "initialize a local blob store",
			Long: "Create a new local content-addressable blob store with " +
				"hash-bucketed directory layout. The store is registered " +
				"under the given blob-store-id and uses the default " +
				"compression and hash settings. The blob-store-id selects " +
				"the XDG scope via an optional prefix ('.', '/', '%', '_', " +
				"or none) — see blob-store(7). Examples: 'default' (XDG " +
				"user), '.archive' (CWD-relative), '%scratch' (XDG cache).",
		},
	})

	utility.AddCmd("init-pointer", &Init{
		tipe: ids.GetOrPanic(
			ids.TypeTomlBlobStoreConfigPointerV0,
		).TypeStruct,
		blobStoreConfig: &blob_store_configs.TomlPointerV0{},
		desc: futility.Description{
			Short: "initialize a pointer blob store",
			Long: "Create a blob store that delegates to another store by " +
				"reference. The pointer store does not hold blobs itself " +
				"but redirects reads and writes to the target store.",
		},
	})

	utility.AddCmd("init-sftp-explicit", &Init{
		tipe: ids.GetOrPanic(
			ids.TypeTomlBlobStoreConfigSftpExplicitV0,
		).TypeStruct,
		blobStoreConfig: &blob_store_configs.TomlSFTPV0{},
		desc: futility.Description{
			Short: "initialize an SFTP blob store with explicit credentials",
			Long: "Create a blob store backed by an SFTP remote, using " +
				"explicitly provided host, port, user, and key path. " +
				"Use -discover to detect an existing remote store's " +
				"configuration from its directory structure.",
		},
	})

	utility.AddCmd("init-sftp-ssh_config", &Init{
		tipe: ids.GetOrPanic(
			ids.TypeTomlBlobStoreConfigSftpViaSSHConfigV0,
		).TypeStruct,
		blobStoreConfig: &blob_store_configs.TomlSFTPViaSSHConfigV0{},
		desc: futility.Description{
			Short: "initialize an SFTP blob store via ssh_config",
			Long: "Create a blob store backed by an SFTP remote, resolving " +
				"connection parameters from ~/.ssh/config host entries. " +
				"Use -discover to detect an existing remote store's " +
				"configuration from its directory structure.",
		},
	})

	utility.AddCmd("init-inventory-archive", &Init{
		tipe: ids.GetOrPanic(
			ids.TypeTomlBlobStoreConfigInventoryArchiveVCurrent,
		).TypeStruct,
		blobStoreConfig: &blob_store_configs.TomlInventoryArchiveV2{
			Delta: blob_store_configs.DeltaConfig{
				Enabled:     false,
				Algorithm:   "bsdiff",
				MinBlobSize: 256,
				MaxBlobSize: 10485760,
				SizeRatio:   2.0,
			},
		},
		desc: futility.Description{
			Short: "initialize an inventory archive blob store",
			Long: "Create a blob store using the inventory archive format, " +
				"which packs blobs into indexed archive files for efficient " +
				"storage and O(1) lookups. This is the current archive " +
				"format version with optional delta compression support.",
		},
	})

	utility.AddCmd("init-inventory-archive-v1", &Init{
		tipe: ids.GetOrPanic(
			ids.TypeTomlBlobStoreConfigInventoryArchiveV1,
		).TypeStruct,
		blobStoreConfig: &blob_store_configs.TomlInventoryArchiveV1{
			Delta: blob_store_configs.DeltaConfig{
				Enabled:     false,
				Algorithm:   "bsdiff",
				MinBlobSize: 256,
				MaxBlobSize: 10485760,
				SizeRatio:   2.0,
			},
		},
		desc: futility.Description{
			Short: "initialize an inventory archive blob store (v1)",
			Long: "Create a blob store using inventory archive format " +
				"version 1 with delta compression support. Prefer " +
				"init-inventory-archive for the current version.",
		},
	})

	utility.AddCmd("init-inventory-archive-v0", &Init{
		tipe: ids.GetOrPanic(
			ids.TypeTomlBlobStoreConfigInventoryArchiveV0,
		).TypeStruct,
		blobStoreConfig: &blob_store_configs.TomlInventoryArchiveV0{},
		desc: futility.Description{
			Short: "initialize an inventory archive blob store (v0)",
			Long: "Create a blob store using the original inventory " +
				"archive format (v0) without delta compression. Prefer " +
				"init-inventory-archive for the current version.",
		},
	})
}

type Init struct {
	tipe            ids.TypeStruct
	blobStoreConfig blob_store_configs.ConfigMutable
	discover        bool
	desc            futility.Description

	command_components.EnvBlobStore
	command_components.Init
}

var (
	_ interfaces.CommandComponentWriter = (*Init)(nil)
	_ futility.CommandWithParams        = (*Init)(nil)
)

func (cmd *Init) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-id",
			Description: "identifier for the new blob store (e.g. 'default', '.archive')",
			Required:    true,
		},
	}
}

func (cmd Init) GetDescription() futility.Description {
	return cmd.desc
}

func (cmd *Init) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
	cmd.blobStoreConfig.SetFlagDefinitions(flagDefinitions)

	if _, isSftp := cmd.blobStoreConfig.(blob_store_configs.ConfigSFTPRemotePath); isSftp {
		flagDefinitions.BoolVar(
			&cmd.discover,
			"discover",
			false,
			"Discover remote blob store config from existing directory structure",
		)
	}
}

func (cmd *Init) Run(req futility.Request) {
	var blobStoreId blob_store_id.Id

	if err := blobStoreId.Set(req.PopArg("blob-store-id")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}

	req.AssertNoMoreArgs()

	tw := tap.NewWriter(os.Stdout)

	if cmd.discover {
		cmd.runDiscover(req, blobStoreId, tw)
		return
	}

	// SFTP-backed stores need a blob_store-config at the remote root
	// before the first write — otherwise reads/writes fail with
	// "remote blob store config missing". Bootstrap one (default-shaped,
	// matching `init -encryption none` for local stores) when the
	// remote doesn't already have one.
	if sftpConfig, ok := cmd.blobStoreConfig.(blob_store_configs.ConfigSFTPRemotePath); ok {
		if !cmd.ensureRemoteConfigExists(req, blobStoreId, sftpConfig) {
			return
		}
	}

	envBlobStore := cmd.MakeEnvBlobStore(req)

	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&blob_store_configs.TypedConfig{
			Type: cmd.tipe,
			Blob: cmd.blobStoreConfig,
		},
	)

	tw.Ok(fmt.Sprintf("init %s", pathConfig.GetConfig()))
	tw.Plan()
}

func (cmd *Init) runDiscover(
	req futility.Request,
	blobStoreId blob_store_id.Id,
	tw *tap.Writer,
) {
	sftpConfig, ok := cmd.blobStoreConfig.(blob_store_configs.ConfigSFTPRemotePath)
	if !ok {
		errors.ContextCancelWithBadRequestError(
			req,
			errors.Errorf("--discover is only supported for SFTP blob stores"),
		)
		return
	}

	printer := ui.MakePrefixPrinter(
		ui.Err(),
		fmt.Sprintf("(blob_store: %s) ", blobStoreId),
	)

	sshClient, err := makeSSHClientForSFTPConfig(req, printer, cmd.blobStoreConfig)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}
	defer sshClient.Close() //defer:err-checked

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		errors.ContextCancelWithBadRequestError(
			req,
			errors.Wrapf(err, "failed to create SFTP client"),
		)
		return
	}

	defer sftpClient.Close() //defer:err-checked

	remotePath := sftpConfig.GetRemotePath()

	// Discover remote config from directory structure
	discovered, err := blob_stores.DiscoverRemoteConfig(sftpClient, remotePath, printer)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}

	tw.Ok(fmt.Sprintf(
		"discovered config: hash=%s buckets=%v multi-hash=%t",
		discovered.HashTypeId,
		discovered.Buckets,
		discovered.MultiHash,
	))

	// Write config to remote
	if err = blob_stores.WriteRemoteConfig(
		sftpClient,
		remotePath,
		discovered,
		printer,
	); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}

	tw.Ok("remote config written")

	// Write local SFTP config pointing to the remote
	envBlobStore := cmd.MakeEnvBlobStore(req)

	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&blob_store_configs.TypedConfig{
			Type: cmd.tipe,
			Blob: cmd.blobStoreConfig,
		},
	)

	tw.Ok(fmt.Sprintf("init %s", pathConfig.GetConfig()))

	// Validate by reading a sample of blobs via the newly configured store
	configNamed := blob_store_configs.ConfigNamed{
		Path: pathConfig,
		Config: blob_store_configs.TypedConfig{
			Type: cmd.tipe,
			Blob: cmd.blobStoreConfig,
		},
	}

	blobStore := blob_stores.MakeRemoteBlobStore(envBlobStore, configNamed)

	var verifiedCount int

	for digest, iterErr := range blobStore.AllBlobs() {
		if iterErr != nil {
			tw.NotOk("blob iteration", map[string]string{"message": iterErr.Error()})
			break
		}

		if err = blob_stores.VerifyBlob(
			req,
			blobStore,
			digest,
			io.Discard,
		); err != nil {
			tw.NotOk(fmt.Sprintf("%s", digest), map[string]string{"message": err.Error()})
			break
		}

		verifiedCount++
		tw.Ok(fmt.Sprintf("verified %s", digest))

		if verifiedCount >= 5 {
			break
		}
	}

	tw.Comment(fmt.Sprintf("verified %d blobs", verifiedCount))
	tw.Plan()
}

// makeSSHClientForSFTPConfig dispatches to the right SSH-client
// constructor based on the concrete SFTP config type. Used by both
// the -discover path and ensureRemoteConfigExists.
func makeSSHClientForSFTPConfig(
	req futility.Request,
	printer ui.Printer,
	blobStoreConfig blob_store_configs.ConfigMutable,
) (sshClient *ssh.Client, err error) {
	switch config := blobStoreConfig.(type) {
	case blob_store_configs.ConfigSFTPUri:
		return blob_stores.MakeSSHClientFromSSHConfig(req, printer, config)

	case blob_store_configs.ConfigSFTPConfigExplicit:
		return blob_stores.MakeSSHClientForExplicitConfig(req, printer, config)

	default:
		return nil, errors.Errorf("unsupported SFTP config type %T", config)
	}
}

// ensureRemoteConfigExists makes sure the SFTP remote has a
// blob_store-config at its root. If one is already there it is left
// alone (someone — a prior init or an external tool — has already
// populated it). If absent, a default TomlV3 config is written
// matching what `madder init -encryption none` produces locally
// (HashTypeDefault, DefaultHashBuckets, CompressionTypeDefault).
//
// The remote directory itself is mkdir'd if missing.
//
// Returns false when the request was cancelled with an error so the
// caller can stop short of writing the local pointer config.
func (cmd *Init) ensureRemoteConfigExists(
	req futility.Request,
	blobStoreId blob_store_id.Id,
	sftpConfig blob_store_configs.ConfigSFTPRemotePath,
) bool {
	printer := ui.MakePrefixPrinter(
		ui.Err(),
		fmt.Sprintf("(blob_store: %s) ", blobStoreId),
	)

	sshClient, err := makeSSHClientForSFTPConfig(req, printer, cmd.blobStoreConfig)
	if err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return false
	}
	defer sshClient.Close() //defer:err-checked

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		errors.ContextCancelWithBadRequestError(
			req,
			errors.Wrapf(err, "failed to create SFTP client"),
		)
		return false
	}
	defer sftpClient.Close() //defer:err-checked

	remotePath := sftpConfig.GetRemotePath()
	configPath := path.Join(remotePath, directory_layout.FileNameBlobStoreConfig)

	if _, statErr := sftpClient.Stat(configPath); statErr == nil {
		printer.Printf("remote blob store config already present at %q", configPath)
		return true
	} else if !os.IsNotExist(statErr) {
		errors.ContextCancelWithBadRequestError(
			req,
			errors.Wrapf(statErr, "failed to stat remote config %q", configPath),
		)
		return false
	}

	if err := sftpClient.MkdirAll(remotePath); err != nil {
		errors.ContextCancelWithBadRequestError(
			req,
			errors.Wrapf(err, "failed to create remote dir %q", remotePath),
		)
		return false
	}

	if err := blob_stores.WriteRemoteConfig(
		sftpClient,
		remotePath,
		blob_stores.DiscoveredConfig{
			HashTypeId: string(blob_store_configs.HashTypeDefault),
			Buckets:    blob_store_configs.DefaultHashBuckets,
		},
		printer,
	); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return false
	}

	return true
}
