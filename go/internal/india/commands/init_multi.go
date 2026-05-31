package commands

import (
	"fmt"
	"os"

	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
	tap "github.com/amarbel-llc/tap/go/pkgs/writer"
)

func init() {
	utility.AddCmd("init-multi", &InitMulti{})
}

type InitMulti struct {
	command_components.EnvBlobStore
	command_components.Init

	mode         string
	writeStore   string
	readStores   []string
	mirrorStores []string
	readFill     bool
	noReadFill   bool
}

var (
	_ interfaces.CommandComponentWriter = (*InitMulti)(nil)
	_ futility.CommandWithParams        = (*InitMulti)(nil)
)

func (cmd *InitMulti) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "blob-store-id",
			Description: "identifier for the new multi store (e.g. '.cache')",
			Required:    true,
		},
	}
}

func (cmd InitMulti) GetDescription() futility.Description {
	return futility.Description{
		Short: "compose existing stores into a multi blob store",
		Long: "Creates a multi blob_store-config that mirrors writes " +
			"across stores or writes through to one store with read " +
			"fallback (and optional cache fill). References are " +
			"recorded as digest-bearing blob-store-ids. See " +
			"docs/features/0009-multi-store-config-type.md.",
	}
}

func (cmd *InitMulti) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.StringVar(&cmd.mode, "mode", "",
		"mirror | write_through")
	flagSet.StringVar(&cmd.writeStore, "write-store", "",
		"write target (write_through mode)")
	// --read-store and --mirror-store are repeatable: flagSet.Func's
	// closure is invoked once per occurrence, mirroring the
	// SetMultiEncryptionFlagDefinition idiom in
	// internal/charlie/blob_store_configs/encryption.go.
	flagSet.Func("read-store",
		"read source (write_through; repeatable)",
		func(value string) error {
			cmd.readStores = append(cmd.readStores, value)
			return nil
		})
	flagSet.Func("mirror-store",
		"mirror member (mirror mode; repeatable)",
		func(value string) error {
			cmd.mirrorStores = append(cmd.mirrorStores, value)
			return nil
		})
	flagSet.BoolVar(&cmd.readFill, "read-fill", false,
		"tee read-source hits into the write store (write_through)")
	flagSet.BoolVar(&cmd.noReadFill, "no-read-fill", false,
		"disable read-fill")
}

func (cmd *InitMulti) Run(req futility.Request) {
	var blobStoreId blob_store_id.Id
	if err := blobStoreId.Set(req.PopArg("blob-store-id")); err != nil {
		errors.ContextCancelWithBadRequestError(req, err)
	}
	req.AssertNoMoreArgs()

	envBlobStore := cmd.MakeEnvBlobStore(req)

	cfg := &blob_store_configs.TomlMultiV0{Mode: cmd.mode}

	resolve := func(ref string) blob_store_id.Id {
		// Resolve a bare name to its leaf's current digest; pass a
		// digest-bearing ref through unchanged. The typed Id renders
		// to the digest-bearing wire form via MarshalText -> Canonical
		// at encode time.
		var id blob_store_id.Id
		if err := id.Set(ref); err != nil {
			errors.ContextCancelWithBadRequestError(req, err)
		}
		if id.HasDigest() {
			return id
		}
		leaf := envBlobStore.GetBlobStore(id)
		digest := leaf.Config.BlobDigest
		if digest.IsNull() {
			req.Cancel(errors.BadRequestf(
				"reference %q targets an unmigrated config; run "+
					"`madder config-pin_digest %s` first", ref, ref))
			return blob_store_id.Id{}
		}
		return id.WithDigest(digest)
	}

	switch cmd.mode {
	case "mirror":
		for _, ref := range cmd.mirrorStores {
			cfg.MirrorStores = append(cfg.MirrorStores, resolve(ref))
		}

	case "write_through":
		cfg.WriteStore = resolve(cmd.writeStore)
		for _, ref := range cmd.readStores {
			cfg.ReadStores = append(cfg.ReadStores, resolve(ref))
		}
		readFill := !cmd.noReadFill // default true unless --no-read-fill
		cfg.ReadFill = &readFill

	default:
		req.Cancel(errors.BadRequestf(
			"--mode must be mirror or write_through, got %q", cmd.mode))
		return
	}

	tw := tap.NewWriter(os.Stdout)

	pathConfig := cmd.InitBlobStore(
		req,
		envBlobStore,
		blobStoreId,
		&blob_store_configs.TypedConfig{
			Type: ids.GetOrPanic(ids.TypeTomlBlobStoreConfigMultiV0).TypeStruct,
			Blob: cfg,
		},
	)

	tw.Ok(fmt.Sprintf("init-multi %s", pathConfig.GetConfig()))
	tw.Plan()
}
