package command_components

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

type ResolvedArg struct {
	Arg           string
	BlobReader    domain_interfaces.BlobReader
	BlobStoreId   blob_store_id.Id
	IsStoreSwitch bool
	Err           error
}

func ResolveFileOrBlobStoreId(arg string) (resolved ResolvedArg) {
	resolved.Arg = arg

	var err error

	if resolved.BlobReader, err = env_dir.NewFileReaderOrErrNotExist(
		env_dir.DefaultConfig,
		arg,
	); errors.IsNotExist(err) {
		if err = resolved.BlobStoreId.Set(arg); err != nil {
			resolved.Err = err
			return resolved
		}

		resolved.IsStoreSwitch = true
		return resolved
	} else if err != nil {
		resolved.Err = err
		return resolved
	}

	return resolved
}
