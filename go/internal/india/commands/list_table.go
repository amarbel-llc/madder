package commands

import (
	"os"
	"path/filepath"
	"strings"

	"code.linenisgreat.com/madder/go/internal/0/xdg_location_type"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/tridex"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"golang.org/x/term"
)

// listSubtleColor dims the table border so it stays legible against both
// light and dark terminal backgrounds (mirrors spinclass's `sc list`
// styling: cmd/spinclass/list_view.go).
var listSubtleColor = lipgloss.AdaptiveColor{Light: "240", Dark: "245"}

var (
	listHeaderStyle     = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	listCellStyle       = lipgloss.NewStyle().Padding(0, 1)
	listBorderStyle     = lipgloss.NewStyle().Foreground(listSubtleColor)
	listIdStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))  // blue (dodder's identifier color)
	listGreyStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("59")) // bright black for @ and digest abbreviations
	listPinnedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	listUnmigratedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))  // yellow
)

// listTableRow is one fully-rendered row of the TTY summary table.
type listTableRow struct {
	id          string
	description string
	path        string
}

// listDescColumn is the 0-based index of the DESCRIPTION column in the
// header order below (ID · DESCRIPTION · PATH · STATUS). It is the only
// column left unpinned, so it is the one lipgloss's resizer grows or
// shrinks to fill the table's target width — and therefore the one that
// wraps (mirrors spinclass's `sc list`: cmd/spinclass/list_view.go's
// fixedColumnWidths/listTableStyleFunc).
const listDescColumn = 1

// listFixedColumnWidths returns the content width (widest cell, header
// included) to pin each non-DESCRIPTION column to. Pinning ID/PATH directs
// all of the table's flex onto DESCRIPTION. Index listDescColumn is left 0
// and unused — that column is never pinned.
func listFixedColumnWidths(rows []listTableRow) [3]int {
	w := [3]int{lipgloss.Width("ID"), 0, lipgloss.Width("PATH")}
	for _, r := range rows {
		w[0] = max(w[0], lipgloss.Width(r.id))
		w[2] = max(w[2], lipgloss.Width(r.path))
	}
	return w
}

// listTableStyleFunc returns the per-cell StyleFunc. Headers get
// listHeaderStyle; when width is known, every column but listDescColumn
// is pinned to its content width (fixed[c]) so DESCRIPTION is the only
// column lipgloss reflows. With width 0 (non-TTY / unknown size) every
// body cell gets the plain padded listCellStyle and the table sizes to
// content, as before.
func listTableStyleFunc(width int, fixed [3]int) table.StyleFunc {
	return func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return listHeaderStyle
		}
		if width > 0 && col != listDescColumn {
			// lipgloss .Width() is the total cell width including padding, so
			// add listCellStyle's horizontal padding back onto the measured
			// content width — otherwise the pinned column truncates its
			// content by the padding.
			return listCellStyle.Width(fixed[col] + listCellStyle.GetHorizontalPadding())
		}
		return listCellStyle
	}
}

// terminalWidth reports stdout's column count for DESCRIPTION wrapping,
// or 0 when stdout is not a sized terminal. renderListTable treats 0 as
// "don't wrap" and falls back to the legacy content-sized layout.
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// abbreviatePath shortens p the way fish's stock prompt_pwd shortens
// $PWD (share/fish/functions/prompt_pwd.fish, default dir-length=1 /
// full-length-dirs=1): $HOME is unexpanded to "~", and every path
// component but the last is trimmed to its leading character (a leading
// dot is preserved for hidden dirs) — so a long XDG-style path like
// ~/.local/share/madder/config collapses to ~/.l/s/m/config while the
// leaf stays legible. home may be "" (skips the tilde step).
func abbreviatePath(home, p string) string {
	return abbreviatePathStyled(home, p, (*lipgloss.Style)(nil))
}

// abbreviatePathStyled is like abbreviatePath but applies style to
// abbreviated path components. style may be nil (no styling applied).
func abbreviatePathStyled(home, p string, style *lipgloss.Style) string {
	if home != "" {
		if p == home {
			p = "~"
		} else if rel, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			p = "~" + string(filepath.Separator) + rel
		}
	}

	parts := strings.Split(p, string(filepath.Separator))
	for i := 0; i < len(parts)-1; i++ {
		original := parts[i]
		shortened := shortenPathComponent(original)
		// If this component was abbreviated (shortened), apply style
		if style != nil && len(shortened) < len(original) {
			parts[i] = style.Render(shortened)
		} else {
			parts[i] = shortened
		}
	}
	return strings.Join(parts, string(filepath.Separator))
}

// shortenPathComponent trims a single path component to its leading
// character, keeping a leading dot (hidden-dir marker) intact. Empty,
// "~", ".", and ".." pass through unchanged.
func shortenPathComponent(c string) string {
	switch {
	case c == "" || c == "~" || c == "." || c == "..":
		return c
	case strings.HasPrefix(c, "."):
		if len(c) <= 2 {
			return c
		}
		return c[:2]
	default:
		return c[:1]
	}
}

// renderListTable renders the styled lipgloss table shown when `madder
// list` runs with -format=auto (the default) on a TTY. width is
// terminalWidth()'s result (0 ⇒ unknown/non-TTY, keeps content-sized
// columns).
func renderListTable(rows []listTableRow, width int) string {
	if len(rows) == 0 {
		return listBorderStyle.Render("No blob stores configured.")
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(listBorderStyle).
		Headers("ID", "DESCRIPTION", "PATH").
		StyleFunc(listTableStyleFunc(width, listFixedColumnWidths(rows)))
	if width > 0 {
		t = t.Width(width)
	}

	for _, r := range rows {
		t.Row(r.id, r.description, r.path)
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
	home := envBlobStore.GetXDG().Home.ActualValue
	stores := stableOrder(blobStores)

	// abbr disambiguates pinned digests the same way markl ids are
	// abbreviated elsewhere: shortest-unique-prefix over the set of
	// digests actually shown, via a tridex. Built once up front so every
	// row's abbreviation stays unique against its siblings, not just
	// itself.
	digests := make([]string, 0, len(stores))
	for _, blobStore := range stores {
		if bd := blobStore.Config.BlobDigest; !bd.IsNull() {
			digests = append(digests, bd.String())
		}
	}
	abbr := tridex.Make(digests...)

	var unmigrated []string
	rows := make([]listTableRow, 0, len(stores))

	for _, blobStore := range stores {
		storeId := blobStore.Path.GetId().String()
		var idDisplay string
		if bd := blobStore.Config.BlobDigest; bd.IsNull() {
			unmigrated = append(unmigrated, storeId)
			idDisplay = listIdStyle.Render(storeId) + " " + listUnmigratedStyle.Render("(unpinned)")
		} else {
			// Blue store ID + greyed @ + italicized greyed digest
			digestAbbr := abbr.Abbreviate(bd.String())
			idDisplay = listIdStyle.Render(storeId) + listGreyStyle.Render("@") + listGreyStyle.Italic(true).Render(digestAbbr)
		}
		// XDG-user-scoped stores live under $HOME regardless of CWD, so
		// their path column is normalized against home ("~/...") instead
		// of the CWD-relative form RelToCwdOrSame produces — which for a
		// store outside the working tree is a long, scannable-only-with-
		// effort chain of "../..". Cwd- and system-scoped stores keep the
		// existing CWD-relative behavior.
		configPath := blobStore.Path.GetConfig()
		if blobStore.Path.GetId().GetLocationType() != xdg_location_type.XDGUser {
			configPath = envBlobStore.RelToCwdOrSame(configPath)
		}
		// Show the directory containing the config file, not the config file itself
		configDir := filepath.Dir(configPath)
		path := abbreviatePathStyled(home, configDir, &listGreyStyle)
		rows = append(rows, listTableRow{
			id:          idDisplay,
			description: blobStore.GetBlobStoreDescription(),
			path:        path,
		})
	}

	envBlobStore.GetUI().Printf("%s", renderListTable(rows, terminalWidth()))
	printUnmigratedNote(envBlobStore, unmigrated)
}
