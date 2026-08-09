package commands

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"code.linenisgreat.com/madder/go/internal/0/xdg_location_type"
	"code.linenisgreat.com/madder/go/internal/bravo/registry"
	"code.linenisgreat.com/madder/go/internal/charlie/output_format"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/tridex"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// registryRow is one fully-assembled row of `madder list -all`. pin and id
// hold the FULL markl-id strings (empty when the config is legacy / unpinned);
// the table renderer abbreviates them by shortest-distinct-prefix, while
// text/json emit them whole for scriptability.
type registryRow struct {
	name        string
	pin         string
	id          string
	tipe        string
	locationDir string // the store's base directory (raw; renderers format it)
	stale       bool
	sortKey     string // registry key / base-path hash, for a stable tiebreak
}

// runAll renders the host-wide registry view: every store registered in the
// per-host index (dodder RFC-0007) unioned with the current scope's discovered
// stores. Advisory only — the index is never consulted for resolution
// (FDR-0010); it just answers "what stores exist on this host".
func (cmd List) runAll(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	home := envBlobStore.GetXDG().Home.ActualValue

	// Best-effort: a missing or unreadable index yields no registered entries,
	// leaving just the current-scope stores — never an error for a listing.
	entries, _ := registry.Entries()

	rows := assembleRegistryRows(envBlobStore, blobStores, entries)

	// auto + TTY renders the styled table natively; every other case (explicit
	// -format, or piped auto which Resolve maps to ndjson) takes the structured
	// path — mirroring List.Run's dispatch.
	if cmd.Format == output_format.FormatAuto && output_format.IsTTY(os.Stdout) {
		envBlobStore.GetUI().Printf("%s", renderRegistryTable(rows, home, terminalWidth()))
		return
	}

	var err error
	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON:
		err = emitRegistryJSON(rows, home)
	case output_format.FormatNDJSON:
		err = emitRegistryNDJSON(rows, home)
	default:
		emitRegistryText(envBlobStore, rows, home)
	}
	if err != nil {
		envBlobStore.Cancel(err)
	}
}

// assembleRegistryRows unions the current scope's live stores (rich, decoded)
// with the registered index entries, deduping by registry key so a store
// present in both appears once (as its richer live row). Registered-only
// entries are decoded from disk; dangling ones become stale rows.
func assembleRegistryRows(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
	entries []registry.Entry,
) []registryRow {
	// Dedup a live current-scope store against its own index entry by BOTH the
	// registry key AND the cleaned config path — belt-and-suspenders against
	// any absolute/relative skew between the base path hashed at init time and
	// the one discovered at list time.
	seenKey := make(map[string]bool)
	seenConfig := make(map[string]bool)
	var rows []registryRow

	for _, bs := range stableOrder(blobStores) {
		seenKey[registry.Key(bs.Path.GetBase())] = true
		seenConfig[filepath.Clean(bs.Path.GetConfig())] = true
		rows = append(rows, rowFromLiveStore(envBlobStore, bs))
	}

	for _, e := range entries {
		if seenKey[e.Key] || seenConfig[filepath.Clean(e.Target)] {
			continue
		}
		if e.Dangling {
			rows = append(rows, staleRow(e))
			continue
		}
		rows = append(rows, rowFromRegisteredConfig(e))
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].name != rows[j].name {
			return rows[i].name < rows[j].name
		}
		return rows[i].sortKey < rows[j].sortKey
	})
	return rows
}

// rowFromLiveStore builds a row from a store discovered in the current scope,
// whose config is already decoded. The store id carries its own scope prefix,
// so NAME is exact here (unlike the registered-only path, which must infer it).
func rowFromLiveStore(
	envBlobStore command_components.BlobStoreEnv,
	bs blob_stores.BlobStoreInitialized,
) registryRow {
	// Mirror emitListTable: XDG-user stores live under $HOME regardless of cwd,
	// so normalize their path against home; cwd/system stores stay cwd-relative.
	configPath := bs.Path.GetConfig()
	if bs.Path.GetId().GetLocationType() != xdg_location_type.XDGUser {
		configPath = envBlobStore.RelToCwdOrSame(configPath)
	}

	return registryRow{
		name:        bs.Path.GetId().String(),
		pin:         digestString(bs.Config.BlobDigest),
		id:          instanceIdString(bs.Config.Blob),
		tipe:        classifyConfig(bs.Config.Blob),
		locationDir: filepath.Dir(configPath),
		sortKey:     registry.Key(bs.Path.GetBase()),
	}
}

// rowFromRegisteredConfig decodes a live (non-dangling) index entry's config
// off disk. A config that exists but cannot be decoded (corrupt, or a newer
// format this binary predates) still lists — with whatever we could not read
// left blank and TYPE "?" — rather than vanishing.
func rowFromRegisteredConfig(e registry.Entry) registryRow {
	storeDir := e.StoreDir()
	row := registryRow{
		name:        inferScopedName(storeDir),
		locationDir: storeDir,
		sortKey:     e.Key,
	}

	typed, err := blob_store_configs.DecodeAndVerifyFromFile(e.Target)
	if err != nil {
		row.tipe = "?"
		return row
	}

	row.pin = digestString(typed.BlobDigest)
	row.id = instanceIdString(typed.Blob)
	row.tipe = classifyConfig(typed.Blob)
	return row
}

// staleRow builds a row for a dangling entry (the store was deleted or moved
// out from under the index). Only the path and inferred name survive; the
// config is gone, so PIN/ID/TYPE are blank and the row is marked stale.
func staleRow(e registry.Entry) registryRow {
	return registryRow{
		name:        inferScopedName(e.StoreDir()),
		locationDir: e.StoreDir(),
		stale:       true,
		sortKey:     e.Key,
	}
}

// digestString renders a config digest, or "" when null (legacy/unpinned).
func digestString(bd markl.Id) string {
	if bd.IsNull() {
		return ""
	}
	return bd.String()
}

// instanceIdString extracts a config's uuidv7 instance id, or "" for a legacy
// config that carries none.
func instanceIdString(blob any) string {
	c, ok := blob.(blob_store_configs.ConfigInstanceId)
	if !ok {
		return ""
	}
	iid := c.GetInstanceId()
	if len(iid.GetBytes()) == 0 {
		return ""
	}
	return iid.String()
}

// classifyConfig maps a decoded config to the short TYPE label the RFC-0007
// table uses. A value has exactly one concrete type, so at most one interface
// case matches; the ordering is specific-before-general only for clarity.
func classifyConfig(blob any) string {
	switch blob.(type) {
	case blob_store_configs.ConfigMulti:
		return "multi"
	case blob_store_configs.ConfigInventoryArchive:
		return "archive"
	case blob_store_configs.ConfigPointer:
		return "pointer"
	case blob_store_configs.ConfigS3:
		return "s3"
	case blob_store_configs.ConfigWebDAV:
		return "webdav"
	case blob_store_configs.ConfigSFTPRemotePath:
		return "sftp"
	default:
		return "local"
	}
}

// inferScopedName reconstructs a store's scoped spelling from its base
// directory path. Best-effort: host-wide there is no marker that distinguishes
// every cwd root, so the scope prefix is inferred from stable path segments and
// falls back to the bare leaf name. LOCATION always carries the exact path, so
// a missed prefix is cosmetic. The live-store path (rowFromLiveStore) has the
// exact scoped id and does not go through here.
func inferScopedName(storeDir string) string {
	name := filepath.Base(storeDir)
	switch {
	case strings.Contains(storeDir, "/.madder/"):
		return "." + name
	case strings.HasPrefix(storeDir, "/var/lib/madder/"),
		strings.Contains(storeDir, "/madder-system/"):
		return "/" + name
	case strings.Contains(storeDir, "/madder-cache/"):
		return "%" + name
	default:
		return name
	}
}

// tildeOnly substitutes $HOME with "~" without the aggressive per-component
// shortening abbreviatePath applies — the plain form used by text/json output.
func tildeOnly(home, p string) string {
	if home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rel
	}
	return p
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// emitRegistryText prints one tab-separated line per store: NAME TYPE PIN ID
// LOCATION, with a "(stale)" marker appended to the name of a dangling entry.
func emitRegistryText(
	envBlobStore command_components.BlobStoreEnv,
	rows []registryRow,
	home string,
) {
	for _, r := range rows {
		name := r.name
		if r.stale {
			name += " (stale)"
		}
		envBlobStore.GetUI().Printf(
			"%s\t%s\t%s\t%s\t%s",
			name,
			orDash(r.tipe),
			orDash(r.pin),
			orDash(r.id),
			tildeOnly(home, r.locationDir),
		)
	}
}

// registryListRecord is the structured (ndjson/json) shape. Emitted as a JSON
// array rather than a map keyed by id, because names collide host-wide (two
// cwd-scoped ".default" stores in different directories) — a keyed object would
// clobber one.
type registryListRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Pin      string `json:"pin,omitempty"`
	Id       string `json:"id,omitempty"`
	Location string `json:"location"`
	Stale    bool   `json:"stale,omitempty"`
}

func makeRegistryRecord(r registryRow, home string) registryListRecord {
	return registryListRecord{
		Name:     r.name,
		Type:     r.tipe,
		Pin:      r.pin,
		Id:       r.id,
		Location: tildeOnly(home, r.locationDir),
		Stale:    r.stale,
	}
}

func emitRegistryNDJSON(rows []registryRow, home string) (err error) {
	buf := bufio.NewWriter(os.Stdout)
	defer errors.DeferredFlusher(&err, buf)

	enc := json.NewEncoder(buf)
	for _, r := range rows {
		_ = enc.Encode(makeRegistryRecord(r, home))
	}
	return nil
}

func emitRegistryJSON(rows []registryRow, home string) (err error) {
	out := make([]registryListRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, makeRegistryRecord(r, home))
	}

	buf := bufio.NewWriter(os.Stdout)
	defer errors.DeferredFlusher(&err, buf)

	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
	return nil
}

// registryLocationColumn is the 0-based index of LOCATION in the header order
// (NAME · PIN · ID · TYPE · LOCATION). It is the only unpinned column, so it is
// the one lipgloss reflows to fill the table width — mirroring the current-scope
// table's treatment of DESCRIPTION (list_table.go).
const registryLocationColumn = 4

func registryFixedColumnWidths(rendered [][5]string) [5]int {
	w := [5]int{
		lipgloss.Width("NAME"),
		lipgloss.Width("PIN"),
		lipgloss.Width("ID"),
		lipgloss.Width("TYPE"),
		0,
	}
	for _, r := range rendered {
		for c := 0; c < registryLocationColumn; c++ {
			w[c] = max(w[c], lipgloss.Width(r[c]))
		}
	}
	return w
}

func registryTableStyleFunc(width int, fixed [5]int) table.StyleFunc {
	return func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return listHeaderStyle
		}
		if width > 0 && col != registryLocationColumn {
			return listCellStyle.Width(fixed[col] + listCellStyle.GetHorizontalPadding())
		}
		return listCellStyle
	}
}

// renderRegistryTable renders the styled `madder list -all` table. pin/id are
// abbreviated by shortest-distinct-prefix (tridex) across the shown set, so
// each abbreviation stays unique against its siblings.
func renderRegistryTable(rows []registryRow, home string, width int) string {
	if len(rows) == 0 {
		return listBorderStyle.Render("No blob stores registered.")
	}

	pins := make([]string, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.pin != "" {
			pins = append(pins, r.pin)
		}
		if r.id != "" {
			ids = append(ids, r.id)
		}
	}
	pinAbbr := tridex.Make(pins...)
	idAbbr := tridex.Make(ids...)

	rendered := make([][5]string, 0, len(rows))
	for _, r := range rows {
		name := listIdStyle.Render(r.name)
		if r.stale {
			name += " " + listUnmigratedStyle.Render("(stale)")
		}

		pin := "—"
		if r.pin != "" {
			pin = listGreyStyle.Italic(true).Render(pinAbbr.Abbreviate(r.pin))
		}
		id := "—"
		if r.id != "" {
			id = listGreyStyle.Italic(true).Render(idAbbr.Abbreviate(r.id))
		}
		tipe := r.tipe
		if tipe == "" {
			tipe = "—"
		}
		loc := abbreviatePathStyled(home, r.locationDir, &listGreyStyle)

		rendered = append(rendered, [5]string{name, pin, id, tipe, loc})
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(listBorderStyle).
		Headers("NAME", "PIN", "ID", "TYPE", "LOCATION").
		StyleFunc(registryTableStyleFunc(width, registryFixedColumnWidths(rendered)))
	if width > 0 {
		t = t.Width(width)
	}

	for _, r := range rendered {
		t.Row(r[0], r[1], r[2], r[3], r[4])
	}

	return t.Render()
}
