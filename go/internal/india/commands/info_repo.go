package commands

import (
	"sort"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/values"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
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
			"With no arguments, shows the default store's immutable config. " +
			"Accepts a blob-store-id and one or more config keys: " +
			"config-immutable (default), config-path, dir-blob_stores, " +
			"xdg, or any key from the store's typed config.",
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
		blobStore = env.GetDefaultBlobStore()
		keys = []string{req.PopArg("blob store config key")}

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
			env.GetUI().Print(
				directory_layout.GetDefaultBlobStore(env).GetConfig(),
			)

		case "dir-blob_stores":
			env.GetUI().Print(env.MakePathBlobStore())

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
