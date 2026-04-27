// Package files provides filesystem helpers used by madder.
//
// Candidate for promotion to dewey: the WriteImmutable helper here
// implements the tmp-write + chmod 0o444 + atomic-rename discipline
// that ADR 0003 already applies to published blobs and ADR 0005 / #65
// extends to blob_store-config files. If the pattern proves stable we
// should lift it into dewey/delta/files.
package files

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// ImmutableFileMode is the permission applied to an immutable file
// once its content has been written. Read-only for owner, group, and
// other. Mirrors the discipline ADR 0003 applies to published blobs.
const ImmutableFileMode os.FileMode = 0o444

// WriteImmutable atomically writes path with content produced by
// write. The write callback receives a writer connected to a tmp
// sibling of path; on success the tmp file is chmod'd to
// ImmutableFileMode and renamed onto path.
//
// Refuses to overwrite an existing path: an immutable artifact's
// identity is its content, so re-writing it is always a bug at the
// caller. Returns os.ErrExist (wrapped) if path already exists.
//
// On error the tmp file is removed before returning.
//
// path's parent directory must already exist.
func WriteImmutable(
	path string,
	write func(io.Writer) error,
) (err error) {
	if _, statErr := os.Lstat(path); statErr == nil {
		err = errors.Wrap(&os.PathError{Op: "write_immutable", Path: path, Err: os.ErrExist})
		return err
	} else if !os.IsNotExist(statErr) {
		err = errors.Wrap(statErr)
		return err
	}

	tmpPath, err := tmpSibling(path)
	if err != nil {
		err = errors.Wrap(err)
		return err
	}

	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	var file *os.File

	if file, err = os.OpenFile(
		tmpPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o666,
	); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = write(file); err != nil {
		_ = file.Close()
		cleanup()
		err = errors.Wrap(err)
		return err
	}

	if err = file.Close(); err != nil {
		cleanup()
		err = errors.Wrap(err)
		return err
	}

	if err = os.Chmod(tmpPath, ImmutableFileMode); err != nil {
		cleanup()
		err = errors.Wrap(err)
		return err
	}

	if err = os.Rename(tmpPath, path); err != nil {
		cleanup()
		err = errors.Wrap(err)
		return err
	}

	return err
}

func tmpSibling(path string) (string, error) {
	var buf [8]byte

	if _, err := rand.Read(buf[:]); err != nil {
		return "", errors.Wrap(err)
	}

	return fmt.Sprintf("%s.tmp_%s", path, hex.EncodeToString(buf[:])), nil
}
