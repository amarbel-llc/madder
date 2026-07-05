package commands

import (
	"io"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/files"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
)

func init() {
	utility.AddCmd("config-pin_digest", &ConfigPinDigest{})
}

// ConfigPinDigest re-emits the named blob_store-config files with
// their @ digest line populated. Idempotent for deterministic
// encoders: a config that already has a matching @ line is decoded
// and re-emitted byte-identical.
type ConfigPinDigest struct {
	command_components.EnvBlobStore

	All bool
}

var _ futility.CommandWithParams = (*ConfigPinDigest)(nil)

func (cmd *ConfigPinDigest) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-id",
			Description: "blob-store-id(s) to migrate; omit when --all is set",
			Variadic:    true,
		},
	}
}

func (cmd ConfigPinDigest) GetDescription() futility.Description {
	return futility.Description{
		Short: "mint or refresh the @ digest line on blob_store-config files",
		Long: "Re-emits the named blob_store-config files with their " +
			"@ digest line populated. Idempotent for deterministic " +
			"encoders: a config that already carries a matching digest " +
			"is re-emitted byte-identical. Pass one or more " +
			"blob-store-ids, or --all to migrate every configured " +
			"blob store. See FDR-0008.",
	}
}

func (cmd *ConfigPinDigest) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.BoolVar(&cmd.All, "all", false,
		"migrate every configured blob_store-config")
}

func (cmd ConfigPinDigest) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)

	hasArgs := req.RemainingArgCount() > 0

	if cmd.All && hasArgs {
		errors.ContextCancelWithBadRequestError(req, errors.Errorf(
			"--all and explicit ids are mutually exclusive",
		))
		return
	}
	if !cmd.All && !hasArgs {
		errors.ContextCancelWithBadRequestError(req, errors.Errorf(
			"specify --all or one or more blob-store-ids",
		))
		return
	}

	allStores := envBlobStore.GetBlobStores()

	var targets blob_stores.BlobStoreMap
	if cmd.All {
		targets = allStores
	} else {
		targets = make(blob_stores.BlobStoreMap, req.RemainingArgCount())
		for range req.RemainingArgCount() {
			id := futility.PopRequestArg[scoped_id.Id](req, "blob-store-id")
			key := id.String()
			bs, ok := allStores[key]
			if !ok {
				envBlobStore.Cancel(errors.Errorf(
					"no such blob store: %q", key,
				))
				return
			}
			targets[key] = bs
		}
	}

	for storeId, target := range targets {
		if err := migrateOneConfig(target); err != nil {
			envBlobStore.Cancel(errors.Wrapf(err,
				"failed to migrate %q", storeId))
			return
		}
	}
}

// migrateOneConfig reads target's blob_store-config, clears its
// BlobDigest (so EncodeWithDigest recomputes), and rewrites the file
// in place via the atomic replace helper.
func migrateOneConfig(target blob_stores.BlobStoreInitialized) error {
	configPath := target.Path.GetConfig()

	typedConfig, err := blob_store_configs.DecodeAndVerifyFromFile(configPath)
	if err != nil {
		return errors.Wrap(err)
	}

	typedConfig.BlobDigest = markl.Id{}

	return files.WriteImmutableReplace(
		configPath,
		func(w io.Writer) error {
			_, err := blob_store_configs.EncodeWithDigest(&typedConfig, w)
			return err
		},
	)
}
