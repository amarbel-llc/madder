package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/fd"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/env_local"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/piggy/go/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
	tap "github.com/amarbel-llc/tap/go/pkgs/writer"
)

func init() {
	utility.AddCmd("init-from", &InitFrom{})
}

type InitFrom struct {
	ifNotExists bool

	command_components.EnvBlobStore
	command_components.Init
}

var (
	_ interfaces.CommandComponentWriter = (*InitFrom)(nil)
	_ futility.CommandWithParams        = (*InitFrom)(nil)
)

func (cmd *InitFrom) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "store-name",
			Description: "name for the new blob store",
			Required:    true,
		},
		futility.Arg[*values.String]{
			Name:        "config-path",
			Description: "path to the blob store configuration file",
			Required:    true,
		},
	}
}

func (cmd InitFrom) GetDescription() futility.Description {
	return futility.Description{
		Short: "initialize a blob store from a configuration file",
		Long: "Create a new blob store by reading its type and settings " +
			"from a hyphence-encoded configuration file. The config is " +
			"automatically upgraded to the current version if an older " +
			"format is detected. Requires a store name and the path to " +
			"the configuration file.",
	}
}

func (cmd *InitFrom) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
	flagDefinitions.BoolVar(
		&cmd.ifNotExists,
		"if-not-exists",
		false,
		"exit 0 (no-op) if the store already exists, instead of erroring. "+
			"Ignored for a digest-pinned id, which is always idempotent "+
			"(and additionally drift-detecting) by digest.",
	)
}

func (cmd InitFrom) Complete(
	req futility.Request,
	envLocal env_local.Env,
	commandLine futility.CommandLineInput,
) {
	// TODO support completion for config path
}

func (cmd *InitFrom) Run(req futility.Request) {
	var blobStoreId scoped_id.Id

	if err := blobStoreId.Set(req.PopArg("blob store name")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}

	var configPathFD fd.FD

	if err := configPathFD.Set(req.PopArg("blob store config path")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}

	req.AssertNoMoreArgs()

	tw := tap.NewWriter(os.Stdout)

	envBlobStore := cmd.MakeEnvBlobStore(req)

	// Pinned id (`name@<digest>`): the two-stage idempotent path. Install
	// the artifact bytes VERBATIM and verify by digest — drift-detecting,
	// and robust against non-deterministic re-encoding (see
	// command_components.EnsureBlobStoreVerbatim).
	if blobStoreId.HasDigest() {
		raw, err := readConfigArtifact(configPathFD)
		if err != nil {
			errors.ContextCancelWithError(req, errors.Wrap(err))
			return
		}

		var artifact blob_store_configs.TypedConfig
		if _, err := blob_store_configs.DecodeAndVerify(
			&artifact,
			bytes.NewReader(raw),
		); err != nil {
			errors.ContextCancelWithError(req, errors.Wrapf(
				err, "config artifact %q", configPathFD.String(),
			))
			return
		}

		if artifact.BlobDigest.IsNull() {
			errors.ContextCancelWithBadRequestf(
				req,
				"config artifact %q has no digest; a pinned init-from needs a "+
					"digest-stamped config (generate one with `madder config-gen`)",
				configPathFD.String(),
			)
			return
		}

		// The id's pin must match the artifact — same assertion the
		// open-by-id path makes (blob_store_env).
		idDigest := blobStoreId.GetDigest()
		if err := markl.AssertEqual(&idDigest, &artifact.BlobDigest); err != nil {
			errors.ContextCancelWithBadRequestf(
				req,
				"id pin %s does not match config artifact digest %s",
				idDigest,
				artifact.BlobDigest,
			)
			return
		}

		pathConfig := cmd.EnsureBlobStoreVerbatim(
			req,
			envBlobStore,
			blobStoreId,
			raw,
			artifact.BlobDigest,
		)

		tw.Ok(fmt.Sprintf("init-from %s", pathConfig.GetConfig()))
		tw.Plan()
		return
	}

	// Unpinned + --if-not-exists: a pre-existing store is a no-op
	// (idempotent-by-existence, no digest check).
	if cmd.ifNotExists {
		path, ok := cmd.ResolveBlobStorePath(envBlobStore, blobStoreId)
		if !ok {
			return
		}

		if _, err := os.Stat(path.GetConfig()); err == nil {
			tw.Ok(fmt.Sprintf("init-from %s (already exists)", path.GetConfig()))
			tw.Plan()
			return
		}
	}

	var typedConfig blob_store_configs.TypedConfig

	{
		var err error

		if typedConfig, err = blob_store_configs.DecodeAndVerifyFromFile(
			configPathFD.String(),
		); err != nil {
			tw.NotOk(
				fmt.Sprintf("init-from %s", configPathFD.String()),
				map[string]string{
					"severity": "fail",
					"message":  err.Error(),
				},
			)
			tw.Plan()
			envBlobStore.Cancel(err)
			return
		}
	}

	for {
		configUpgraded, ok := typedConfig.Blob.(blob_store_configs.ConfigUpgradeable)

		if !ok {
			break
		}

		typedConfig.Blob, typedConfig.Type = configUpgraded.Upgrade()
	}

	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&typedConfig,
	)

	tw.Ok(fmt.Sprintf("init-from %s", pathConfig.GetConfig()))
	tw.Plan()
}

// readConfigArtifact reads the raw bytes of a config-gen artifact for the
// pinned (verbatim) path, mirroring DecodeAndVerifyFromFile's "-" = stdin
// convention. The raw bytes are written to the store unchanged, so the
// on-disk digest equals the artifact's.
func readConfigArtifact(configPathFD fd.FD) ([]byte, error) {
	if configPathFD.String() == "-" {
		return io.ReadAll(os.Stdin)
	}

	return os.ReadFile(configPathFD.String())
}
