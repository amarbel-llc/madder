package directory_layout

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/files"
	"github.com/amarbel-llc/purse-first/libs/dewey/echo/xdg"
)

const (
	FileNameBlobStoreConfig       = "blob_store-config"
	fileNameBlobStoreConfigLegacy = "dodder-blob_store-config"
)

func GetBlobStoreConfigPaths(
	ctx interfaces.ActiveContext,
	directoryLayout BlobStore,
) []string {
	legacyPaths := GetLegacyBlobStoreConfigPaths(ctx, directoryLayout)

	if len(legacyPaths) > 0 {
		ctx.Cancel(errors.Errorf(
			"found legacy blob store config file(s) at:\n\t%s\n"+
				"rename each to %q to continue",
			strings.Join(legacyPaths, "\n\t"),
			FileNameBlobStoreConfig,
		))

		return nil
	}

	globPattern := DirBlobStore(
		directoryLayout,
		fmt.Sprintf("*/%s", FileNameBlobStoreConfig),
	)

	var configPaths []string

	{
		var err error

		if configPaths, err = filepath.Glob(globPattern); err != nil {
			ctx.Cancel(err)
			return configPaths
		}
	}

	return configPaths
}

func GetLegacyBlobStoreConfigPaths(
	ctx interfaces.ActiveContext,
	directoryLayout BlobStore,
) []string {
	globPattern := DirBlobStore(
		directoryLayout,
		fmt.Sprintf("*/%s", fileNameBlobStoreConfigLegacy),
	)

	legacyPaths, err := filepath.Glob(globPattern)
	if err != nil {
		ctx.Cancel(err)
		return nil
	}

	return legacyPaths
}

func PathBlobStore(
	layout BlobStore,
	targets ...string,
) interfaces.DirectoryLayoutPath {
	return layout.MakePathBlobStore(targets...)
}

func DirBlobStore(
	layout BlobStore,
	targets ...string,
) string {
	return PathBlobStore(layout, targets...).String()
}

// FindAllCwdOverridePaths walks up from cwd and returns every ancestor
// that contains `.<utilityName>/` (file or directory), deepest-first.
// Honors `<UTILITYNAME>_CEILING_DIRECTORIES` (parsed via dewey's
// xdg.ParseCeilingDirectories + xdg.IsAboveCeiling).
//
// Multi-match counterpart of dewey's xdg.getCwdXDGOverridePath, which
// returns only the deepest match. Loop shape mirrors the post-#75
// dewey: the ceiling dir is the LAST dir checked, not the first
// excluded — match git's GIT_CEILING_DIRECTORIES semantics. See #145
// and amarbel-llc/purse-first#75.
//
// Both cwd and each ceiling are resolved through filepath.EvalSymlinks
// before the walk, matching git(1)'s documented behavior for
// GIT_CEILING_DIRECTORIES: "Normally, Git has to read the entries in
// this list and resolve any symlink that might be present in order to
// compare them with the current directory." Without symmetric resolution
// on both sides, macOS's /var → /private/var symlink (and similar)
// leaves ceiling and walked dir in incompatible string forms, so the
// walk never stops. In production this canonicalization is a no-op:
// os.Getwd() already returns the canonical form on macOS and Linux, so
// returned ancestor paths look the same as before. Tracked upstream in
// dewey: ceiling resolution should live in xdg.IsAboveCeiling itself.
// See amarbel-llc/purse-first#80.
func FindAllCwdOverridePaths(
	cwd, utilityName string,
	ceilings []string,
) []string {
	if cwd == "" || utilityName == "" {
		return nil
	}

	marker := "." + utilityName
	resolvedCwd := resolveSymlinksBestEffort(cwd)
	resolvedCeilings := resolveCeilings(ceilings)

	var ancestors []string

	dir := resolvedCwd
	for safety := 0; safety < 100; safety++ {
		if files.Exists(filepath.Join(dir, marker)) {
			ancestors = append(ancestors, dir)
		}

		if dir == string(filepath.Separator) {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent

		if xdg.IsAboveCeiling(dir, resolvedCeilings) {
			break
		}
	}

	return ancestors
}

// resolveCeilings runs each ceiling through filepath.EvalSymlinks so
// that path-string comparison in xdg.IsAboveCeiling sees the same form
// for both sides regardless of intermediate symlinks. Entries that fail
// to resolve (e.g. the ceiling doesn't exist on disk) are passed through
// unchanged so a user-supplied non-existent ceiling still bounds the
// walk by name — matches git's best-effort behavior.
func resolveCeilings(ceilings []string) []string {
	if len(ceilings) == 0 {
		return ceilings
	}

	out := make([]string, len(ceilings))
	for i, c := range ceilings {
		out[i] = resolveSymlinksBestEffort(c)
	}

	return out
}

func resolveSymlinksBestEffort(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}
