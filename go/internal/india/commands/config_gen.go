package commands

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

func init() {
	utility.AddCmd(
		"config-gen",
		&ConfigGen{
			tipe: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct,
			blobStoreConfig: &blob_store_configs.DefaultType{
				HashTypeId:      blob_store_configs.HashTypeDefault,
				HashBuckets:     blob_store_configs.DefaultHashBuckets,
				CompressionType: "zstd",
			},
		},
	)
}

// ConfigGen is stage one of the two-stage init flow: it emits a
// digest-pinned blob_store-config artifact WITHOUT touching any store or
// env. Stage two (`init-from <id>@<digest> <config>`) binds the artifact to
// a store, idempotently and drift-detecting. ConfigGen mirrors `init`'s
// default local config type; remote types (SFTP/WebDAV/S3) are not yet
// surfaced here (no current driver).
type ConfigGen struct {
	tipe            ids.TypeStruct
	blobStoreConfig blob_store_configs.ConfigMutable
}

var _ futility.CommandWithParams = (*ConfigGen)(nil)

func (cmd *ConfigGen) GetParams() []futility.Param { return nil }

func (cmd ConfigGen) GetDescription() futility.Description {
	return futility.Description{
		Short: "generate a digest-pinned blob_store-config artifact",
		Long: "Emit a blob_store-config to stdout with its FDR-0008 content " +
			"digest stamped into the metadata, WITHOUT creating any store. " +
			"This is stage one of the two-stage init flow: capture the " +
			"artifact, then bind it to a store with `madder init-from " +
			"<id>@<digest> <config>`, which is idempotent and drift-" +
			"detecting (re-running asserts the on-disk store still matches " +
			"the artifact). The config is deterministic for the default " +
			"local store type, so the same flags always yield the same " +
			"digest — suitable for pinning in a deploy (e.g. a systemd " +
			"ExecStartPre baked at build time). The resolved digest is also " +
			"printed to stderr for convenience.",
	}
}

func (cmd *ConfigGen) SetFlagDefinitions(
	flagDefinitions interfaces.CLIFlagDefinitions,
) {
	cmd.blobStoreConfig.SetFlagDefinitions(flagDefinitions)
}

func (cmd *ConfigGen) Run(req futility.Request) {
	req.AssertNoMoreArgs()

	typedConfig := blob_store_configs.TypedConfig{
		Type: cmd.tipe,
		Blob: cmd.blobStoreConfig,
	}

	// EncodeWithDigest stamps typedConfig.BlobDigest as it writes (covering
	// the body bytes), so the artifact on stdout carries its own @digest.
	if _, err := blob_store_configs.EncodeWithDigest(
		&typedConfig,
		os.Stdout,
	); err != nil {
		errors.ContextCancelWithError(req, err)
		return
	}

	// Surface the resolved digest on stderr so a deploy can capture it for
	// the `init-from <id>@<digest>` pin without re-parsing the artifact.
	// This is exactly the string scoped_id parses after `@`.
	fmt.Fprintln(os.Stderr, typedConfig.BlobDigest.String())
}
