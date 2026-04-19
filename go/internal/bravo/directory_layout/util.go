package directory_layout

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
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
				"run `madder migrate-legacy-configs` to rename them to %q",
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

func RenameLegacyBlobStoreConfig(legacyPath string) (newPath string, err error) {
	dir := filepath.Dir(legacyPath)
	base := filepath.Base(legacyPath)

	if base != fileNameBlobStoreConfigLegacy {
		return "", errors.Errorf(
			"path %q does not have legacy filename %q",
			legacyPath,
			fileNameBlobStoreConfigLegacy,
		)
	}

	newPath = filepath.Join(dir, FileNameBlobStoreConfig)

	return newPath, nil
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
