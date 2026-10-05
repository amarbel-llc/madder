package blob_stores

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"code.linenisgreat.com/madder/go/internal/0/domain_interfaces"
	"code.linenisgreat.com/madder/go/internal/alfa/markl_io"
	"code.linenisgreat.com/madder/go/internal/alfa/scoped_id"
	"code.linenisgreat.com/madder/go/internal/delta/blob_store_configs"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_io"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/files"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/interfaces"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ohio"
)

type localHashBucketed struct {
	config blob_store_configs.ConfigLocalHashBucketed

	id scoped_id.Id

	multiHash         bool
	defaultHashFormat markl.FormatHash
	buckets           []int

	basePath string
	tempFS   env_dir.TemporaryFS

	// verifyOnCollision is the OR of the per-store config flag and the
	// MADDER_VERIFY_ON_COLLISION env-var override, resolved at store
	// construction. See ADR 0003 and issue #31.
	verifyOnCollision bool

	// observer is captured from the ambient env at store construction
	// and forwarded into blob_io.MoveOptions on every MakeBlobWriter.
	// Nil means no audit logging for this store's writes. See ADR 0004.
	observer domain_interfaces.BlobWriteObserver

	// sealedKey is set for a sealed-key store (FDR 0011) and replaces the
	// config's encryption id on every read and write. Nil for every other
	// store, including ones built directly in tests.
	sealedKey domain_interfaces.MarklId
}

var (
	_ domain_interfaces.BlobStore                 = localHashBucketed{}
	_ BlobDeleter                                 = localHashBucketed{}
	_ domain_interfaces.BlobForeignDigestAdder    = localHashBucketed{}
	_ domain_interfaces.BlobForeignDigestResolver = localHashBucketed{}
)

func makeLocalHashBucketed(
	envDir env_dir.Env,
	id scoped_id.Id,
	basePath string,
	config blob_store_configs.ConfigLocalHashBucketed,
) (store localHashBucketed, err error) {
	store.config = config
	store.id = id

	store.multiHash = config.SupportsMultiHash()
	if store.defaultHashFormat, err = markl.GetFormatHashOrError(
		config.GetDefaultHashTypeId(),
	); err != nil {
		err = errors.Wrap(err)
		return store, err
	}
	store.buckets = config.GetHashBuckets()

	store.basePath = basePath
	// Per ADR 0003: the tempFS is XDG_CACHE_HOME-rooted (or its CWD-scoped
	// override). Cache and data are assumed to live on the same filesystem;
	// if that invariant is violated, link(2) in blob_mover returns EXDEV
	// and the caller gets a clear error pointing at ADR 0003 and
	// blob-store(7).
	store.tempFS = envDir.GetTempLocal()

	// Per issue #31: either the store config or the runtime env var
	// override enables byte-level collision verification on EEXIST.
	store.verifyOnCollision = config.GetVerifyOnCollision() ||
		envDir.GetVerifyOnCollisionOverride()

	// Per ADR 0004: pull the audit-log observer off the ambient env and
	// forward it to every MoveOptions built from this store.
	store.observer = envDir.GetBlobWriteObserver()

	if sealedKeyConfig, ok := config.(blob_store_configs.ConfigSealedKey); ok {
		if store.sealedKey, err = makeSealedKeyEncryption(
			id.String(),
			sealedKeyConfig,
			loadLocalStoreKeySidecar(basePath),
		); err != nil {
			err = errors.Wrap(err)
			return store, err
		}
	}

	return store, err
}

// blobEncryption is the encryption id reads and writes use: the sealed
// key for a sealed-key store, else whatever the config declares.
func (blobStore localHashBucketed) blobEncryption() domain_interfaces.MarklId {
	if blobStore.sealedKey != nil {
		return blobStore.sealedKey
	}

	return blobStore.config.GetBlobEncryption()
}

var _ StoreKeySidecarStore = localHashBucketed{}

func (blobStore localHashBucketed) ReadStoreKeySidecar() ([]byte, error) {
	return os.ReadFile(localStoreKeySidecarPath(blobStore.basePath))
}

func (blobStore localHashBucketed) WriteStoreKeySidecar(sidecar []byte) error {
	return WriteLocalStoreKeySidecar(
		localStoreKeySidecarPath(blobStore.basePath),
		sidecar,
	)
}

func (blobStore localHashBucketed) GetBlobStoreConfig() blob_store_configs.Config {
	return blobStore.config
}

func (blobStore localHashBucketed) GetBlobStoreDescription() string {
	return "local hash bucketed"
}

func (blobStore localHashBucketed) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	return blobStore.config
}

func (blobStore localHashBucketed) GetDefaultHashType() domain_interfaces.FormatHash {
	return blobStore.defaultHashFormat
}

func (blobStore localHashBucketed) makeEnvDirConfig(
	hashFormat domain_interfaces.FormatHash,
) blob_io.Config {
	if hashFormat == nil {
		hashFormat = blobStore.defaultHashFormat
	}

	return blob_io.MakeConfig(
		hashFormat,
		blob_io.MakeHashBucketPathJoinFunc(blobStore.buckets),
		blobStore.config.GetBlobCompression(),
		blobStore.blobEncryption(),
	)
}

func (blobStore localHashBucketed) HasBlob(
	merkleId domain_interfaces.MarklId,
) (ok bool) {
	if merkleId.IsNull() {
		ok = true
		return ok
	}

	path := blob_io.MakeHashBucketPathFromMerkleId(
		merkleId,
		blobStore.buckets,
		blobStore.multiHash,
		blobStore.basePath,
	)

	ok = files.Exists(path)

	return ok
}

func (blobStore localHashBucketed) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	if blobStore.multiHash {
		return localAllBlobsMultihash(blobStore.basePath)
	} else {
		return localAllBlobs(blobStore.basePath, blobStore.defaultHashFormat)
	}
}

func (blobStore localHashBucketed) MakeBlobReader(
	digest domain_interfaces.MarklId,
) (readCloser domain_interfaces.BlobReader, err error) {
	if digest.IsNull() {
		hash, _ := blobStore.defaultHashFormat.Get() //repool:owned
		readCloser = markl_io.MakeNopReadCloser(
			hash,
			ohio.NopCloser(bytes.NewReader(nil)),
		)
		return readCloser, err
	}

	if readCloser, err = blobStore.blobReaderFrom(
		digest,
		blobStore.basePath,
	); err != nil {
		if !blob_io.IsErrBlobMissing(err) {
			err = errors.Wrap(err)
		}

		return readCloser, err
	}

	return readCloser, err
}

func (blobStore localHashBucketed) MakeBlobWriter(
	marklHashType domain_interfaces.FormatHash,
) (blobWriter domain_interfaces.BlobWriter, err error) {
	if blobWriter, err = blobStore.blobWriterTo(
		blobStore.basePath,
		marklHashType,
	); err != nil {
		err = errors.Wrap(err)
		return blobWriter, err
	}

	return blobWriter, err
}

func (blobStore localHashBucketed) blobWriterTo(
	path string,
	hashFormat domain_interfaces.FormatHash,
) (mover domain_interfaces.BlobWriter, err error) {
	if hashFormat == nil {
		hashFormat = blobStore.defaultHashFormat
	}

	if blobStore.multiHash {
		path = filepath.Join(
			path,
			hashFormat.GetMarklFormatId(),
		)
	}

	if mover, err = blob_io.NewMover(
		blobStore.makeEnvDirConfig(hashFormat),
		blob_io.MoveOptions{
			FinalPathOrDir:              path,
			GenerateFinalPathFromDigest: true,
			TemporaryFS:                 blobStore.tempFS,
			VerifyOnCollision:           blobStore.verifyOnCollision,
			Observer:                    blobStore.observer,
			StoreId:                     blobStore.id.String(),
		},
	); err != nil {
		err = errors.Wrap(err)
		return mover, err
	}

	return mover, err
}

func (blobStore localHashBucketed) blobReaderFrom(
	digest domain_interfaces.MarklId,
	basePath string,
) (readCloser domain_interfaces.BlobReader, err error) {
	if digest.IsNull() {
		hash, _ := blobStore.defaultHashFormat.Get() //repool:owned
		readCloser = markl_io.MakeNopReadCloser(
			hash,
			ohio.NopCloser(bytes.NewReader(nil)),
		)
		return readCloser, err
	}

	// Verify under the digest's own hash type — shared with the remote
	// stores via readHashFormatForDigest.
	var hashFormat markl.FormatHash
	if hashFormat, err = readHashFormatForDigest(digest); err != nil {
		return readCloser, err
	}

	basePath = blob_io.MakeHashBucketPathFromMerkleId(
		digest,
		blobStore.buckets,
		blobStore.multiHash,
		basePath,
	)

	if readCloser, err = blob_io.NewFileReaderOrErrNotExist(
		blobStore.makeEnvDirConfig(hashFormat),
		basePath,
	); err != nil {
		if errors.IsNotExist(err) {
			err = blob_io.ErrBlobMissing{
				BlobId: func() domain_interfaces.MarklId { id, _ := markl.Clone(digest); return id }(), //repool:owned
				Path:   basePath,
			}
		} else {
			err = errors.Wrapf(
				err,
				"Path: %q, Compression: %q",
				basePath,
				blobStore.config.GetBlobCompression(),
			)
		}

		return readCloser, err
	}

	return readCloser, err
}

func (blobStore localHashBucketed) DeleteBlob(
	id domain_interfaces.MarklId,
) (err error) {
	path := blob_io.MakeHashBucketPathFromMerkleId(
		id,
		blobStore.buckets,
		blobStore.multiHash,
		blobStore.basePath,
	)

	if err = os.Remove(path); err != nil {
		err = errors.Wrapf(err, "deleting blob %s", id)
		return err
	}

	return nil
}

func (blobStore localHashBucketed) AddForeignBlobDigestForNativeDigest(
	foreign domain_interfaces.MarklId,
	native domain_interfaces.MarklId,
) (err error) {
	if !blobStore.multiHash {
		err = errors.Errorf(
			"single-hash store does not support foreign digest mapping",
		)
		return err
	}

	nativePath := blob_io.MakeHashBucketPathFromMerkleId(
		native,
		blobStore.buckets,
		blobStore.multiHash,
		blobStore.basePath,
	)

	foreignPath := blob_io.MakeHashBucketPathFromMerkleId(
		foreign,
		blobStore.buckets,
		blobStore.multiHash,
		blobStore.basePath,
	)

	foreignDir := filepath.Dir(foreignPath)

	if err = os.MkdirAll(foreignDir, os.ModeDir|0o755); err != nil {
		err = errors.Wrap(err)
		return err
	}

	var relTarget string

	if relTarget, err = filepath.Rel(foreignDir, nativePath); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = os.Symlink(relTarget, foreignPath); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}

// ResolveForeignBlobDigest reads back the alias AddForeignBlobDigestForNativeDigest
// wrote: the foreign path is a relative symlink into the native hash type's
// tree, so its target path parses back into the native digest the same way
// AllBlobs parses blob paths. See madder#285.
func (blobStore localHashBucketed) ResolveForeignBlobDigest(
	foreign domain_interfaces.MarklId,
) (native domain_interfaces.MarklId, ok bool, err error) {
	if !blobStore.multiHash || foreign.IsNull() {
		return native, ok, err
	}

	foreignPath := blob_io.MakeHashBucketPathFromMerkleId(
		foreign,
		blobStore.buckets,
		blobStore.multiHash,
		blobStore.basePath,
	)

	var info os.FileInfo

	if info, err = os.Lstat(foreignPath); err != nil {
		if errors.IsNotExist(err) {
			err = nil
		} else {
			err = errors.Wrap(err)
		}

		return native, ok, err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return native, ok, err
	}

	var relTarget string

	if relTarget, err = os.Readlink(foreignPath); err != nil {
		err = errors.Wrap(err)
		return native, ok, err
	}

	// The adder always writes a relative target, but a hand-planted alias
	// may be absolute, and Join would silently graft it under the alias's
	// directory instead of treating it as a root.
	nativePath := relTarget

	if !filepath.IsAbs(nativePath) {
		nativePath = filepath.Join(filepath.Dir(foreignPath), relTarget)
	}

	var relNative string

	if relNative, err = filepath.Rel(blobStore.basePath, nativePath); err != nil {
		err = errors.Wrap(err)
		return native, ok, err
	}

	hashTypeId, _, _ := strings.Cut(relNative, string(filepath.Separator))

	if hashTypeId == ".." || hashTypeId == relNative {
		err = errors.Errorf(
			"foreign digest alias %q points outside the store's hash-type trees: %q",
			foreignPath,
			relTarget,
		)

		return native, ok, err
	}

	var hashType markl.FormatHash

	if hashType, err = markl.GetFormatHashOrError(hashTypeId); err != nil {
		err = errors.Wrapf(err, "foreign digest alias %q", foreignPath)
		return native, ok, err
	}

	id, repool := hashType.GetBlobId()
	defer repool()

	if err = markl.SetHexStringFromAbsolutePath(
		id,
		nativePath,
		filepath.Join(blobStore.basePath, hashTypeId),
	); err != nil {
		err = errors.Wrapf(err, "foreign digest alias %q", foreignPath)
		return native, ok, err
	}

	native, _ = markl.Clone(id) //repool:owned
	ok = true

	return native, ok, err
}
