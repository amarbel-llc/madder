package commands

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
)

func init() {
	// TODO rename to repo-info
	utility.AddCmd("info-repo", &InfoRepo{})
}

type InfoRepo struct {
	command_components.EnvBlobStore
	command_components.BlobStore
}

var _ futility.CommandWithParams = (*InfoRepo)(nil)

func (cmd *InfoRepo) GetParams() []futility.Param {
	return []futility.Param{
		futility.Arg[*values.String]{
			Name:        "store-id",
			Description: "blob store to query (defaults to default store)",
		},
		futility.Arg[*values.String]{
			Name:        "keys",
			Description: "config keys to display (defaults to config-immutable)",
			Variadic:    true,
		},
	}
}

func (cmd InfoRepo) GetDescription() futility.Description {
	return futility.Description{
		Short: "display blob store configuration",
		Long: "Show the configuration of a blob store in hyphence format. " +
			"With no arguments, shows the default store's immutable " +
			"config. With one argument, the value is tried as a " +
			"blob-store-id first; if no store matches, it is treated as " +
			"a config key against the default store. With two or more, " +
			"the first is the blob-store-id and the rest are config " +
			"keys: config-immutable (default), config-path, " +
			"dir-blob_stores, xdg, or any key from the store's typed " +
			"config.",
	}
}

func (cmd InfoRepo) Run(req futility.Request) {
	env := cmd.MakeEnvBlobStore(req)

	var blobStore blob_stores.BlobStoreInitialized
	var keys []string

	switch req.RemainingArgCount() {
	case 0:
		blobStore = env.GetDefaultBlobStore()
		keys = []string{"config-immutable"}

	case 1:
		arg := req.PopArg("blob store index or config key")

		if matched, ok := lookupBlobStoreById(env, arg); ok {
			blobStore = matched
			keys = []string{"config-immutable"}
		} else {
			blobStore = env.GetDefaultBlobStore()
			keys = []string{arg}
		}

	case 2:
		blobStoreIndex := req.PopArg("blob store index")
		blobStore = cmd.MakeBlobStoreFromIdString(env, blobStoreIndex)
		keys = []string{req.PopArg("blob store config key")}

	default:
		blobStoreIndex := req.PopArg("blob store index")
		blobStore = cmd.MakeBlobStoreFromIdString(env, blobStoreIndex)
		keys = req.PopArgs()
	}

	blobStoreConfig := blobStore.Config
	configKVs := blob_store_configs.ConfigKeyValues(blobStoreConfig.Blob)

	// Lazy: GetBlobStoreConfig() forces a remote read for SFTP. Pulling
	// it only when a key actually needs it keeps transport-only and
	// pseudo-key requests off the network.
	var (
		storeConfig    blob_store_configs.Config
		storeConfigKVs map[string]string
	)

	getStoreConfig := func() (blob_store_configs.Config, map[string]string) {
		if storeConfig == nil {
			storeConfig = blobStore.BlobStore.GetBlobStoreConfig()
			storeConfigKVs = blob_store_configs.ConfigKeyValues(storeConfig)
		}

		return storeConfig, storeConfigKVs
	}

	for _, key := range keys {
		switch strings.ToLower(key) {
		case "config-immutable":
			// Per ADR 0005 §"info-repo … config-immutable wire shape"
			// (#78), this pseudo-key encodes BlobStore.GetBlobStoreConfig()
			// only. Wrap the freestanding Config back into a TypedBlob
			// with the matching wire type-id so the hyphence Coder can
			// route it to the right per-type encoder.
			cfg, _ := getStoreConfig()

			immutableTyped := &hyphence.TypedBlob[blob_store_configs.Config]{
				Type: blob_store_configs.TypeStructForConfig(cfg),
				Blob: cfg,
			}

			if _, err := blob_store_configs.Coder.EncodeTo(
				immutableTyped,
				env.GetUIFile(),
			); err != nil {
				env.Cancel(err)
			}

		case "config-path":
			env.GetUI().Print(blobStore.Path.GetConfig())

		case "dir-blob_stores":
			env.GetUI().Print(filepath.Dir(blobStore.Path.GetBase()))

		case "xdg":
			ecksDeeGee := env.GetXDG()

			dotenv := xdg.Dotenv{
				XDG: &ecksDeeGee,
			}

			if _, err := dotenv.WriteTo(env.GetUIFile()); err != nil {
				env.Cancel(err)
			}

		default:
			// Local-first lookup. The local typed config holds transport
			// keys (host, port, user, …); GetBlobStoreConfig() holds
			// blob-store properties (hash, buckets, compression). For
			// SFTP that means consulting the remote — only worth the
			// SSH dial on a miss.
			if value, ok := configKVs[key]; ok {
				env.GetUI().Print(value)
				continue
			}

			cfg, kvs := getStoreConfig()
			if value, ok := kvs[key]; ok {
				env.GetUI().Print(value)
				continue
			}

			availableKeys := mergeKeyNames(
				blob_store_configs.ConfigKeyNames(blobStoreConfig.Blob),
				blob_store_configs.ConfigKeyNames(cfg),
			)

			errors.ContextCancelWithBadRequestf(
				env,
				"unsupported info key: %q\navailable keys: %s",
				key,
				strings.Join(availableKeys, ", "),
			)

			return
		}
	}
}

// lookupBlobStoreById probes the configured blob-store map for an
// id-shaped string without cancelling the env on a miss. Used by the
// 1-arg path where the same positional could be a store-id or a
// config key — Cancel-on-miss is wrong because the caller will fall
// back to interpreting the arg as a key.
func lookupBlobStoreById(
	env command_components.BlobStoreEnv,
	arg string,
) (blob_stores.BlobStoreInitialized, bool) {
	var id blob_store_id.Id

	if err := id.Set(arg); err != nil {
		return blob_stores.BlobStoreInitialized{}, false
	}

	stores := env.GetBlobStores()
	bs, ok := stores[id.String()]

	return bs, ok
}

// mergeKeyNames returns the deduplicated, sorted union of two
// already-sorted key-name lists. Used to surface every key the user
// could have asked for when info-repo rejects an unknown key — the
// local-typed config and the blob-store-properties config can each
// contribute different keys (transport vs. backend properties for
// SFTP per ADR 0005).
func mergeKeyNames(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	merged := make([]string, 0, len(a)+len(b))

	for _, list := range [][]string{a, b} {
		for _, name := range list {
			if _, ok := seen[name]; ok {
				continue
			}

			seen[name] = struct{}{}
			merged = append(merged, name)
		}
	}

	sort.Strings(merged)

	return merged
}
