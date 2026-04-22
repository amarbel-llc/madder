package env_dir

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
)

// TODO move into own package

type MoveOptions struct {
	TemporaryFS
	FinalPathOrDir              string
	GenerateFinalPathFromDigest bool
	// VerifyOnCollision, when true, invokes checkCollision on the
	// link(2) EEXIST branch — opening both the temp file and the
	// existing blob as BlobReaders and byte-comparing their decoded
	// streams. On mismatch, returns ErrCollisionContentMismatch.
	// See ADR 0003 and issue #31.
	VerifyOnCollision bool
}

type localFileMover struct {
	funcJoin func(string, ...string) string
	file     *os.File
	domain_interfaces.BlobWriter

	basePath          string
	blobPath          string
	verifyOnCollision bool

	// envDirConfig is retained from construction so the EEXIST branch
	// can build a BlobReader over the existing blob with the same
	// decoding config (compression/encryption) the writer used.
	envDirConfig Config
}

func NewMover(
	config Config,
	moveOptions MoveOptions,
) (domain_interfaces.BlobWriter, error) {
	// TODO make MoveOptions an interface and add support for localFileShaMover
	// and localFinalPathMover
	return newMover(config, moveOptions)
}

// TODO split mover into sha-based mover and final-path based mover
// TODO extract writer portion in injected depenency
func newMover(
	config Config,
	moveOptions MoveOptions,
) (mover *localFileMover, err error) {
	mover = &localFileMover{
		funcJoin:          config.funcJoin,
		verifyOnCollision: moveOptions.VerifyOnCollision,
		envDirConfig:      config,
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

// blobFileMode is the mode applied to the temp file immediately before it is
// linked into the content-addressed tree. 0o444 means the final inode is
// read-only to everyone from birth — there is no mutable window between
// publish and lock. See ADR 0003.
const blobFileMode os.FileMode = 0o444

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

	// fsync file data to disk before link so a crash between link and the
	// next fsync cannot leave a zero-byte file at blobPath.
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

	tempPath := mover.file.Name()

	// Per ADR 0003: chmod the temp file read-only *before* link so the
	// inode published at blobPath is immutable from birth. No transient
	// writable window exists.
	if err = os.Chmod(tempPath, blobFileMode); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// Per ADR 0003: link(2) is the publish primitive. On EEXIST a same-digest
	// writer already published; by ADR 0002 the bytes are equivalent so the
	// race is benign. Unlink the temp and return cleanly.
	linkErr := os.Link(tempPath, mover.blobPath)
	switch {
	case linkErr == nil:
		// happy path — fall through to unlink the temp and fsync the dir.
	case errors.Is(linkErr, fs.ErrExist):
		// A same-digest writer published first. By ADR 0002 the bytes
		// are equivalent and this is benign — unless the store opted
		// into byte-level verification (issue #31), in which case we
		// open both paths as BlobReaders and confirm the decoded
		// streams match.
		if mover.verifyOnCollision {
			if err = verifyExistingBlobMatches(
				mover.envDirConfig,
				tempPath,
				mover.blobPath,
			); err != nil {
				// Leave the temp file on disk for forensics; caller
				// who opted into verification wants evidence of the
				// collision, not silent cleanup.
				err = errors.Wrap(err)
				return err
			}
		}
		if err = os.Remove(tempPath); err != nil {
			err = errors.Wrap(err)
			return err
		}
		return err
	case errors.Is(linkErr, syscall.EXDEV):
		err = errors.Wrapf(
			linkErr,
			"blob temp dir and blob store base path are on different "+
				"filesystems; link(2) cannot cross mount boundaries. "+
				"See docs/decisions/0003-blob-store-hardlink-writes.md "+
				"and blob-store(7) for the same-filesystem invariant. "+
				"tempPath=%q blobPath=%q",
			tempPath,
			mover.blobPath,
		)
		return err
	default:
		err = errors.Wrap(linkErr)
		return err
	}

	if err = os.Remove(tempPath); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// fsync the parent directory so the link's directory-entry update is
	// durable across crashes. POSIX does not persist link metadata until
	// the containing directory is fsynced.
	if err = fsyncDir(filepath.Dir(mover.blobPath)); err != nil {
		err = errors.Wrap(err)
		return err
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
