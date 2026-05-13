package commands

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
)

func init() {
	utility.AddCmd("list", &List{Format: output_format.Default})
}

type List struct {
	command_components.EnvBlobStore

	Format output_format.Format
}

var (
	_ interfaces.CommandComponentWriter = (*List)(nil)
	_ futility.CommandWithParams        = (*List)(nil)
)

func (cmd *List) GetParams() []futility.Param { return nil }

func (cmd List) GetDescription() futility.Description {
	return futility.Description{
		Short: "list configured blob stores",
		Long: "List all blob stores configured for the current repository, " +
			"showing each store's ID, description, and the location of its " +
			"on-disk config file. In text mode each line is " +
			"'<id>: <description> # path: <rel>' where <rel> is the " +
			"config-file path expressed relative to the current working " +
			"directory (absolute fallback). In JSON mode (-format=json) " +
			"each store emits an NDJSON record with fields \"id\", " +
			"\"description\", \"config_path\" (absolute), and \"base\" " +
			"(absolute). Output defaults to text on an interactive terminal " +
			"and to NDJSON when stdout is piped; pass -format to force a " +
			"specific encoding. Store IDs use prefixes to indicate scope: " +
			"unprefixed for XDG user stores, '.' for CWD-relative stores, " +
			"and '/' for XDG system stores.",
	}
}

func (cmd *List) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
}

type listRecord struct {
	Id          string `json:"id"`
	Description string `json:"description"`
	ConfigPath  string `json:"config_path"`
	Base        string `json:"base"`
}

func (cmd List) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		emitListJSON(blobStores)
	default:
		emitListText(envBlobStore, blobStores)
	}
}

func emitListText(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	cwd, _ := os.Getwd()

	for _, blobStore := range blobStores {
		envBlobStore.GetUI().Printf(
			"%s: %s # path: %s",
			blobStore.Path.GetId(),
			blobStore.GetBlobStoreDescription(),
			relOrAbs(cwd, blobStore.Path.GetConfig()),
		)
	}
}

func emitListJSON(blobStores blob_stores.BlobStoreMap) {
	buf := bufio.NewWriter(os.Stdout)
	defer buf.Flush()

	enc := json.NewEncoder(buf)

	for _, blobStore := range blobStores {
		_ = enc.Encode(listRecord{
			Id:          blobStore.Path.GetId().String(),
			Description: blobStore.GetBlobStoreDescription(),
			ConfigPath:  blobStore.Path.GetConfig(),
			Base:        blobStore.Path.GetBase(),
		})
	}
}

// relOrAbs returns abs expressed relative to cwd, or abs unchanged if
// cwd is empty or filepath.Rel rejects the inputs. The relative form
// is the friendly rendering for humans running list inside a repo;
// the absolute fallback keeps text-mode output usable in pathological
// setups. JSON mode always emits absolute paths.
func relOrAbs(cwd, abs string) string {
	if cwd == "" {
		return abs
	}

	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}

	return rel
}
