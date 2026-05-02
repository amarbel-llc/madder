package hyphence

import (
	"bytes"
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/files"
)

func DecodeFromFileInto[
	BLOB any,
	BLOB_PTR interfaces.Ptr[BLOB],
](
	typedBlob *TypedBlob[BLOB],
	coders CoderToTypedBlob[BLOB],
	path string,
) (err error) {
	var file *os.File

	if path == "-" {
		file = os.Stdin
	} else {
		if file, err = files.OpenExclusiveReadOnly(
			path,
		); err != nil {
			err = errors.Wrap(err)
			return err
		}

		defer errors.DeferredCloser(&err, file)
	}

	if _, err = coders.DecodeFrom(typedBlob, file); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}

func DecodeFromFile[
	BLOB any,
	BLOB_PTR interfaces.Ptr[BLOB],
](
	coders CoderToTypedBlob[BLOB],
	path string,
) (typedBlob TypedBlob[BLOB], err error) {
	if err = DecodeFromFileInto(&typedBlob, coders, path); err != nil {
		err = errors.Wrap(err)
		return typedBlob, err
	}

	return typedBlob, err
}

// EncodeToFile is the symmetric counterpart to DecodeFromFile: writes
// `typedBlob` through `coders.EncodeTo` to the file at `path`. A path of
// "-" routes to stdout (the encode-side mirror of DecodeFromFileInto's
// `path == "-"` → stdin shortcut). For real paths, the file is created
// with `files.CreateExclusiveWriteOnly` so racing a concurrent writer
// fails fast rather than producing a torn write.
//
// Originally lived here, removed in d583ed8 ("Closes #95: drop dead
// EncodeToFile") when the last in-tree caller (the local blob_store-config
// write) moved to files.WriteImmutable. Re-added because dodder's
// hyphence-via-pkgs migration (issue #107, dodder #144) re-introduces
// genuine callers — at least the slash-and-burn branch's
// `internal/golf/env_repo/blob_store.go:222` config persistence path.
func EncodeToFile[
	BLOB any,
	BLOB_PTR interfaces.Ptr[BLOB],
](
	coders CoderToTypedBlob[BLOB],
	typedBlob *TypedBlob[BLOB],
	path string,
) (err error) {
	var file *os.File

	if path == "-" {
		file = os.Stdout
	} else {
		if file, err = files.CreateExclusiveWriteOnly(
			path,
		); err != nil {
			err = errors.Wrap(err)
			return err
		}

		defer errors.DeferredCloser(&err, file)
	}

	if _, err = coders.EncodeTo(typedBlob, file); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}

// For decodes where typedBlob.Blob should be populated with an empty struct
// when the file is missing
func DecodeFromFileOrEmptyBuffer[
	BLOB any,
	BLOB_PTR interfaces.Ptr[BLOB],
](
	coders CoderToTypedBlob[BLOB],
	path string,
	permitNotExist bool,
) (typedBlob TypedBlob[BLOB], err error) {
	typedBlob, err = DecodeFromFile(coders, path)

	if err == nil {
		return typedBlob, err
	}

	if _, err = coders.DecodeFrom(&typedBlob, bytes.NewBuffer(nil)); err != nil {
		err = errors.Wrap(err)
		return typedBlob, err
	}

	return typedBlob, err
}

