package directory_layout

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/files"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
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
// xdg.IsAboveCeiling symlink-resolves both dir and each ceiling before
// comparison (amarbel-llc/purse-first#80), so this wrapper no longer
// needs to canonicalize either side.
func FindAllCwdOverridePaths(
	cwd, utilityName string,
	ceilings []string,
) []string {
	if cwd == "" || utilityName == "" {
		return nil
	}

	marker := "." + utilityName

	var ancestors []string

	dir := cwd
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

		if xdg.IsAboveCeiling(dir, ceilings) {
			break
		}
	}

	return ancestors
}
