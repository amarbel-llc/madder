package commands

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/charlie/fd"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/env_local"
	"code.linenisgreat.com/madder/go/internal/futility"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/madder/go/internal/hotel/blob_transfers"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/values"
	tap "code.linenisgreat.com/tap/go/pkgs/writer"
)

func init() {
	utility.AddCmd("init-from", &InitFrom{})
}

type InitFrom struct {
	ifNotExists bool
	fromStore   string
	sync        bool

	command_components.EnvBlobStore
	command_components.Init
	command_components.BlobStore
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
			Description: "path to the blob store configuration file (omit when using --from-store)",
			Required:    false,
		},
	}
}

func (cmd InitFrom) GetDescription() futility.Description {
	return futility.Description{
		Short: "initialize a blob store from a config file or another store",
		Long: "Create a new blob store by reading its type and settings " +
			"from a hyphence-encoded configuration file (the config-path " +
			"argument), or from an existing store's config with " +
			"--from-store <store-id>. In either case the config is " +
			"automatically upgraded to the current version if an older " +
			"format is detected.\n\n" +
			"--from-store performs a copy-migration (FDR-0010): the new " +
			"store is a fresh instance with its own minted uuid identity " +
			"(and thus a distinct config digest); the source store is left " +
			"untouched. Add --sync to also copy the source store's blobs " +
			"into the new store. The new store id must not be digest-pinned " +
			"in --from-store mode, since its digest is minted at creation.",
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

	flagDefinitions.StringVar(
		&cmd.fromStore,
		"from-store",
		"",
		"source the new store's config from an existing store (a "+
			"blob-store-id) instead of a config-path argument, upgrading it "+
			"and minting a fresh instance identity (FDR-0010 copy-migration). "+
			"Mutually exclusive with the config-path argument.",
	)

	flagDefinitions.BoolVar(
		&cmd.sync,
		"sync",
		false,
		"with --from-store, also copy the source store's blobs into the new "+
			"store after creating it.",
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

	// Exactly one of {config-path argument, --from-store} selects the config
	// source. The config-path arg is optional at the param level so the
	// --from-store form can omit it; this is the enforcement.
	hasConfigPath := req.RemainingArgCount() > 0

	if cmd.fromStore != "" && hasConfigPath {
		errors.ContextCancelWithBadRequestError(req, errors.Errorf(
			"a config-path argument and --from-store are mutually exclusive",
		))
		return
	}

	if cmd.fromStore == "" && !hasConfigPath {
		errors.ContextCancelWithBadRequestError(req, errors.Errorf(
			"provide a config-path argument or --from-store <store-id>",
		))
		return
	}

	if cmd.fromStore != "" {
		cmd.runFromStore(req, blobStoreId)
		return
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

// runFromStore implements the --from-store copy-migration (FDR-0010): read the
// source store's on-disk config independently, upgrade it to the current
// version, clear its instance-id so InitBlobStore's EncodeWithDigest funnel
// mints a FRESH uuid (making the new store a distinct instance, not a clone of
// the source's identity), create the new store, and — with --sync — copy the
// source's blobs into it. The source store's config file is only read, never
// written, so the source is left byte-untouched.
func (cmd *InitFrom) runFromStore(req futility.Request, newId scoped_id.Id) {
	req.AssertNoMoreArgs()

	// A migration mints a fresh digest at creation, so the new id cannot be
	// digest-pinned in advance.
	if newId.HasDigest() {
		errors.ContextCancelWithBadRequestError(req, errors.Errorf(
			"init-from --from-store mints a fresh digest at creation; the new "+
				"store id %q must not be digest-pinned",
			newId,
		))
		return
	}

	var oldId scoped_id.Id

	if err := oldId.Set(cmd.fromStore); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
		return
	}

	tw := tap.NewWriter(os.Stdout)
	envBlobStore := cmd.MakeEnvBlobStore(req)

	oldPath, ok := cmd.ResolveBlobStorePath(envBlobStore, oldId)
	if !ok {
		return
	}

	var typedConfig blob_store_configs.TypedConfig

	{
		var err error

		if typedConfig, err = blob_store_configs.DecodeAndVerifyFromFile(
			oldPath.GetConfig(),
		); err != nil {
			envBlobStore.Cancel(errors.Wrapf(
				err,
				"source store %q config at %q",
				oldId,
				oldPath.GetConfig(),
			))
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

	// Force a fresh instance identity: clearing the source-inherited
	// instance-id makes EncodeWithDigest's mint funnel treat this as a new
	// store and mint a new uuid. Every current-version store config is
	// mintable; the guard keeps this robust if a future non-mintable current
	// config ever exists.
	if mintable, ok := typedConfig.Blob.(blob_store_configs.ConfigInstanceIdMintable); ok {
		mintable.SetInstanceId(markl.Id{})
	}

	pathConfig := cmd.InitBlobStore(req, envBlobStore, newId, &typedConfig)

	if !cmd.sync {
		tw.Ok(fmt.Sprintf("init-from %s", pathConfig.GetConfig()))
		tw.Plan()
		return
	}

	// --sync: copy the source store's blobs into the new store. Open both by
	// id from disk (MakeBlobStoreByScopedId re-reads the config, so it sees
	// the store init just wrote).
	source := cmd.MakeBlobStoreByScopedId(envBlobStore, oldId)
	destination := cmd.MakeBlobStoreByScopedId(envBlobStore, newId)

	counts, err := blob_transfers.CopyAllBlobs(envBlobStore, source, destination)
	if err != nil {
		envBlobStore.Cancel(errors.Wrapf(
			err, "copying blobs from %q to %q", oldId, newId,
		))
		return
	}

	tw.Ok(fmt.Sprintf(
		"init-from %s (copied %d, already present %d, total %d)",
		pathConfig.GetConfig(),
		counts.Succeeded,
		counts.Ignored,
		counts.Total,
	))
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
