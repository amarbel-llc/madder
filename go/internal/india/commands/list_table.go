package commands

import (
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_stores"
	"github.com/amarbel-llc/madder/go/internal/golf/command_components"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// listSubtleColor dims the table border so it stays legible against both
// light and dark terminal backgrounds (mirrors spinclass's `sc list`
// styling: cmd/spinclass/list_view.go).
var listSubtleColor = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}

var (
	listHeaderStyle     = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	listCellStyle       = lipgloss.NewStyle().Padding(0, 1)
	listBorderStyle     = lipgloss.NewStyle().Foreground(listSubtleColor)
	listPinnedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	listUnmigratedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
)

// listTableRow is one fully-rendered row of the TTY summary table. status
// carries digest state as a styled string ("pinned" / "unmigrated") rather
// than the full digest, keeping the table scannable — the full digest
// remains available via -format=ndjson/json.
type listTableRow struct {
	id          string
	description string
	path        string
	status      string
}

func listTableStyleFunc(row, _ int) lipgloss.Style {
	if row == table.HeaderRow {
		return listHeaderStyle
	}
	return listCellStyle
}

// renderListTable renders the styled lipgloss table shown when `madder
// list` runs with -format=auto (the default) on a TTY.
func renderListTable(rows []listTableRow) string {
	if len(rows) == 0 {
		return listBorderStyle.Render("No blob stores configured.")
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(listBorderStyle).
		Headers("ID", "DESCRIPTION", "PATH", "STATUS").
		StyleFunc(listTableStyleFunc)

	for _, r := range rows {
		t.Row(r.id, r.description, r.path, r.status)
	}

	return t.Render()
}

// emitListTable is the auto+TTY presentation of `madder list`: a styled
// table plus the same unmigrated-store remediation note emitListText
// prints beneath its plain-text lines.
func emitListTable(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) {
	var unmigrated []string
	rows := make([]listTableRow, 0, len(blobStores))

	for _, blobStore := range stableOrder(blobStores) {
		idStr := blobStore.Path.GetId().String()
		status := listPinnedStyle.Render("pinned")
		if blobStore.Config.BlobDigest.IsNull() {
			status = listUnmigratedStyle.Render("unmigrated")
			unmigrated = append(unmigrated, idStr)
		}
		rows = append(rows, listTableRow{
			id:          idStr,
			description: blobStore.GetBlobStoreDescription(),
			path:        envBlobStore.RelToCwdOrSame(blobStore.Path.GetConfig()),
			status:      status,
		})
	}

	envBlobStore.GetUI().Printf("%s", renderListTable(rows))
	printUnmigratedNote(envBlobStore, unmigrated)
}
