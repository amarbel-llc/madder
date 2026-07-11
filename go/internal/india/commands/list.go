package commands

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/charlie/output_format"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/futility"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
)

func init() {
	utility.AddCmd("list", &List{Format: output_format.Default})
}

type List struct {
	command_components.EnvBlobStore

	Format output_format.Format
	Tree   bool
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
			"directory (absolute fallback). In ndjson mode (-format=ndjson) " +
			"each store emits one JSON object per line with fields \"id\", " +
			"\"description\", \"config_path\" (absolute), and \"base\" " +
			"(absolute). In json mode (-format=json) the same per-store " +
			"records are emitted as values of a single top-level JSON " +
			"object keyed by store ID (the PWD-resolved form, e.g. " +
			"\".default\"). Output defaults to a styled table on an " +
			"interactive terminal and to ndjson when stdout is piped; " +
			"pass -format=tap to force the plain '<id>: <description> " +
			"# path: <rel>' text lines regardless of TTY, or -format to " +
			"pick another encoding. Store IDs use prefixes to indicate " +
			"scope: unprefixed for XDG user stores, '.' for CWD-relative " +
			"stores, and '/' for XDG system stores.",
	}
}

func (cmd *List) SetFlagDefinitions(
	flagSet interfaces.CLIFlagDefinitions,
) {
	flagSet.Var(&cmd.Format, "format", output_format.FlagDescription)
	flagSet.BoolVar(&cmd.Tree, "tree", false,
		"render the multi-store reference graph (forces text output)")
}

type listRecord struct {
	Id            string          `json:"id"`
	Description   string          `json:"description"`
	ConfigPath    string          `json:"config_path"`
	Base          string          `json:"base"`
	Digest        string          `json:"digest,omitempty"`
	DigestMissing bool            `json:"digest_missing,omitempty"`
	Mode          string          `json:"mode,omitempty"`
	ReadFill      *bool           `json:"read_fill,omitempty"`
	Refs          []listRecordRef `json:"refs,omitempty"`
}

type listRecordRef struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Role   string `json:"role"` // "write" | "read" | "mirror"
}

func (cmd List) Run(req futility.Request) {
	envBlobStore := cmd.MakeEnvBlobStore(req)
	blobStores := envBlobStore.GetBlobStores()

	// -tree is a human-facing text rendering of the multi-store graph;
	// it forces text output regardless of -format or whether stdout is
	// a TTY. The structured graph data (mode/read_fill/refs) remains
	// available via -format=ndjson/json without -tree. See #225.
	if cmd.Tree {
		emitListTree(envBlobStore, blobStores)
		return
	}

	// auto + TTY renders the styled lipgloss table natively (mirrors
	// sync's viewport guard, sync.go:266-273); -format=tap explicitly
	// requested still yields the legacy plain-text lines regardless of
	// TTY, so scripts redirecting `-format=tap` output stay stable.
	if cmd.Format == output_format.FormatAuto && output_format.IsTTY(os.Stdout) {
		emitListTable(envBlobStore, blobStores)
		return
	}

	// ndjson emits one record per line; json wraps the same records in
	// a single top-level object keyed by store id.
	var err error
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		err = emitListJSONObject(blobStores)
	case output_format.FormatNDJSON:
		err = emitListNDJSON(blobStores)
	case output_format.FormatTAP:
		emitListText(envBlobStore, blobStores)
	default:
		emitListText(envBlobStore, blobStores)
	}
	if err != nil {
		req.Cancel(err)
	}
}

func emitListText(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	var unmigrated []string

	for _, blobStore := range stableOrder(blobStores) {
		idStr := blobStore.Path.GetId().String()
		bd := blobStore.Config.BlobDigest
		if !bd.IsNull() {
			idStr += "@" + bd.String()
		} else {
			idStr += " (unmigrated)"
			unmigrated = append(unmigrated, blobStore.Path.GetId().String())
		}
		envBlobStore.GetUI().Printf(
			"%s: %s # path: %s",
			idStr,
			blobStore.GetBlobStoreDescription(),
			envBlobStore.RelToCwdOrSame(blobStore.Path.GetConfig()),
		)
	}

	printUnmigratedNote(envBlobStore, unmigrated)
}

// printUnmigratedNote prints the remediation hint shared by the text and
// table renderers when one or more stores are missing tamper-detection
// digests. No-op when unmigrated is empty.
func printUnmigratedNote(
	envBlobStore command_components.BlobStoreEnv,
	unmigrated []string,
) {
	if len(unmigrated) == 0 {
		return
	}
	envBlobStore.GetUI().Printf("")
	envBlobStore.GetUI().Printf(
		"NOTE: %d store(s) above are missing tamper-detection digests.",
		len(unmigrated),
	)
	envBlobStore.GetUI().Printf("      Run this to migrate them:")
	envBlobStore.GetUI().Printf("")
	envBlobStore.GetUI().Printf(
		"        madder config-pin_digest %s",
		strings.Join(unmigrated, " "),
	)
}

func emitListNDJSON(blobStores blob_stores.BlobStoreMap) (err error) {
	buf := bufio.NewWriter(os.Stdout)
	defer errors.DeferredFlusher(&err, buf)

	enc := json.NewEncoder(buf)

	for _, blobStore := range stableOrder(blobStores) {
		_ = enc.Encode(makeListRecord(blobStore))
	}
	return nil
}

func emitListJSONObject(blobStores blob_stores.BlobStoreMap) (err error) {
	out := make(map[string]listRecord, len(blobStores))

	for _, blobStore := range blobStores {
		id := blobStore.Path.GetId().String()
		out[id] = makeListRecord(blobStore)
	}

	buf := bufio.NewWriter(os.Stdout)
	defer errors.DeferredFlusher(&err, buf)

	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return nil
}

func makeListRecord(blobStore blob_stores.BlobStoreInitialized) listRecord {
	rec := listRecord{
		Id:          blobStore.Path.GetId().String(),
		Description: blobStore.GetBlobStoreDescription(),
		ConfigPath:  blobStore.Path.GetConfig(),
		Base:        blobStore.Path.GetBase(),
	}
	bd := blobStore.Config.BlobDigest
	if !bd.IsNull() {
		rec.Digest = blob_store_configs.DigestPurpose + "@" + bd.String()
	} else {
		rec.DigestMissing = true
	}

	if multi, ok := blobStore.Config.Blob.(blob_store_configs.ConfigMulti); ok {
		rec.Mode = multi.GetMode()
		switch multi.GetMode() {
		case "mirror":
			for _, id := range multi.GetMirrorStores() {
				rec.Refs = append(rec.Refs, makeRef(id, "mirror"))
			}
		case "write_through":
			rec.Refs = append(rec.Refs, makeRef(multi.GetWriteStore(), "write"))
			for _, id := range multi.GetReadStores() {
				rec.Refs = append(rec.Refs, makeRef(id, "read"))
			}
			rf := multi.GetReadFill()
			rec.ReadFill = &rf
		}
	}

	return rec
}

// makeRef splits an already-parsed, digest-bearing reference into its
// bare name (the BlobStoreMap key) and digest. The ids are typed and
// digest-bearing by construction (decode-time Validate), so no parsing
// or error handling is needed.
func makeRef(id scoped_id.Id, role string) listRecordRef {
	return listRecordRef{
		Name:   id.String(),
		Digest: id.GetDigest().String(),
		Role:   role,
	}
}

// emitListTree renders each store as a line, and for multi stores walks
// its references and prints each referenced leaf indented beneath it
// with a role annotation. Referenced stores are looked up in the same
// BlobStoreMap by their bare id (id.String()).
func emitListTree(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	for _, blobStore := range stableOrder(blobStores) {
		idStr := blobStore.Path.GetId().String()
		bd := blobStore.Config.BlobDigest
		if !bd.IsNull() {
			idStr += "@" + bd.String()
		} else {
			idStr += " (unmigrated)"
		}

		multi, isMulti := blobStore.Config.Blob.(blob_store_configs.ConfigMulti)
		if !isMulti {
			envBlobStore.GetUI().Printf(
				"%s: %s",
				idStr,
				blobStore.GetBlobStoreDescription(),
			)
			continue
		}

		envBlobStore.GetUI().Printf(
			"%s: %s [multi/%s]",
			idStr,
			blobStore.GetBlobStoreDescription(),
			multi.GetMode(),
		)

		var refs []listRecordRef
		switch multi.GetMode() {
		case "mirror":
			for _, id := range multi.GetMirrorStores() {
				refs = append(refs, makeRef(id, "mirror"))
			}
		case "write_through":
			refs = append(refs, makeRef(multi.GetWriteStore(), "write"))
			for _, id := range multi.GetReadStores() {
				refs = append(refs, makeRef(id, "read"))
			}
		}

		for _, ref := range refs {
			description := ""
			if child, ok := blobStores[ref.Name]; ok {
				description = child.GetBlobStoreDescription()
			}
			envBlobStore.GetUI().Printf(
				"  └── %s  %s  (%s)",
				ref.Name,
				description,
				ref.Role,
			)
		}
	}
}
