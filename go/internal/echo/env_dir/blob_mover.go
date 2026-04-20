package env_dir

import (
	"os"
	"path/filepath"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/files"
)

// TODO move into own package

type MoveOptions struct {
	TemporaryFS
	FinalPathOrDir              string
	GenerateFinalPathFromDigest bool
}

type localFileMover struct {
	funcJoin func(string, ...string) string
	file     *os.File
	domain_interfaces.BlobWriter

	basePath string
	blobPath string
	lockFile bool
}

func NewMover(
	config Config,
	moveOptions MoveOptions,
) (domain_interfaces.BlobWriter, error) {
	// TODO make MoveOptions an interface and add support for localFileShaMover
	// and localFinalPathMover
	return newMover(config, moveOptions)
}

// TODO add back support for locking internal files
// TODO split mover into sha-based mover and final-path based mover
// TODO extract writer portion in injected depenency
func newMover(
	config Config,
	moveOptions MoveOptions,
) (mover *localFileMover, err error) {
	mover = &localFileMover{
		funcJoin: config.funcJoin,
	}

	if moveOptions.GenerateFinalPathFromDigest {
		mover.basePath = moveOptions.FinalPathOrDir

		if mover.basePath == "" {
			err = errors.ErrorWithStackf("basepath is nil")
			return mover, err
		}
	} else {
		mover.blobPath = moveOptions.FinalPathOrDir
	}

	if mover.file, err = moveOptions.FileTemp(); err != nil {
		err = errors.Wrap(err)
		return mover, err
	}

	if mover.BlobWriter, err = NewWriter(
		config,
		mover.file,
	); err != nil {
		err = errors.Wrap(err)
		return mover, err
	}

	return mover, err
}

func (mover *localFileMover) Close() (err error) {
	if mover.file == nil {
		err = errors.ErrorWithStackf("nil file")
		return err
	}

	if mover.BlobWriter == nil {
		err = errors.ErrorWithStackf("nil object reader")
		return err
	}

	if err = mover.BlobWriter.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// fsync file data to disk before rename so a crash between rename and
	// the next fsync cannot leave a zero-byte file at blobPath.
	if err = mover.file.Sync(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = mover.file.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	digest := mover.GetMarklId()

	if err = markl.MakeErrEmptyType(digest); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if digest.IsNull() {
		return err
	}

	// if err = merkle.MakeErrIsNull(digest, ""); err != nil {
	// 	err = errors.Wrap(err)
	// 	return
	// }

	// log.Log().Printf(
	// 	"wrote %d bytes to %s, sha %s",
	// 	fi.Size(),
	// 	m.file.Name(),
	// 	sh,
	// )

	if mover.blobPath == "" {
		// TODO-P3 move this validation to options
		if mover.blobPath, err = MakeDirIfNecessary(
			markl.FormatBytesAsHex(digest),
			mover.funcJoin,
			mover.basePath,
		); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	path := mover.file.Name()

	// Per ADR 0002 (content-addressed overwrite-is-fine): os.Rename silently
	// replaces any existing file at mover.blobPath, which is the desired
	// behaviour for a strong-hash content-addressed store. A racing writer
	// that produced the same digest produced the same bytes, so the
	// replacement is semantically a no-op.
	if err = os.Rename(path, mover.blobPath); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// fsync the parent directory so the rename's directory-entry update is
	// durable across crashes. POSIX does not persist rename metadata until
	// the containing directory is fsynced.
	if err = fsyncDir(filepath.Dir(mover.blobPath)); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if mover.lockFile {
		if err = files.SetDisallowUserChanges(mover.blobPath); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}

func fsyncDir(path string) (err error) {
	var dir *os.File

	if dir, err = os.Open(path); err != nil {
		err = errors.Wrap(err)
		return err
	}

	defer errors.DeferredCloser(&err, dir)

	if err = dir.Sync(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}
