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
// xdg.ParseCeilingDirectories + xdg.IsAtOrAboveCeiling).
//
// This is the multi-match counterpart of dewey's
// xdg.getCwdXDGOverridePath, which only returns the deepest match.
// Loop shape and ceiling semantics deliberately mirror dewey so a
// future dewey API can subsume this helper. See #145.
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
	for safety := 0; safety < 1024; safety++ {
		if files.Exists(filepath.Join(dir, marker)) {
			ancestors = append(ancestors, dir)
		}

		if dir == string(filepath.Separator) {
			break
		}

		parent := filepath.Dir(dir)

		if xdg.IsAtOrAboveCeiling(parent, ceilings) {
			break
		}

		if parent == dir {
			break
		}

		dir = parent
	}

	return ancestors
}
