package blob_stores

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ohio"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
)

type remoteSftp struct {
	ctx       interfaces.ActiveContext
	uiPrinter ui.Printer
	once      sync.Once

	id blob_store_id.Id

	buckets []int

	config blob_store_configs.ConfigSFTPRemotePath

	// remoteConfig is the authoritative blob-store-properties config
	// decoded from the SFTP remote's blob_store-config file. Per ADR
	// 0005, the local `config` (TomlSFTPV0) above is transport only;
	// hash type, buckets, compression, and encryption all live here.
	// Populated by readRemoteConfig; nil before initializeOnce runs.
	remoteConfig blob_store_configs.Config

	multiHash       bool
	defaultHashType markl.FormatHash

	// blobIOWrapper holds the remote config's compression / encryption
	// view per ADR 0005. Populated by readRemoteConfig; nil before
	// initializeOnce runs.
	blobIOWrapper        domain_interfaces.BlobIOWrapper
	sshClientInitializer func() (*ssh.Client, error)
	sshClient            *ssh.Client
	sftpClient           *sftp.Client

	// observer receives one BlobWriteEvent per successful upload. Set
	// at store-construction time from envDir.GetBlobWriteObserver().
	// Nil means no audit logging; the mover's emitWriteEvent handles
	// that case cleanly. Per ADR 0004 and issue #50 this currently
	// only reports op=written — existing-target and error dispositions
	// are tracked as a follow-up.
	observer domain_interfaces.BlobWriteObserver

	// TODO extract below into separate struct
	blobCacheLock sync.RWMutex
	blobCache     map[string]struct{}
}

var _ domain_interfaces.BlobStore = &remoteSftp{}

func makeSftpStore(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	id blob_store_id.Id,
	config blob_store_configs.ConfigSFTPRemotePath,
	sshClientInitializer func() (*ssh.Client, error),
	observer domain_interfaces.BlobWriteObserver,
) (blobStore *remoteSftp, err error) {
	var defaultHashType markl.FormatHash

	if defaultHashType, err = markl.GetFormatHashOrError(
		blob_store_configs.DefaultHashTypeId,
	); err != nil {
		err = errors.Wrap(err)
		return blobStore, err
	}

	blobStore = &remoteSftp{
		ctx:                  ctx,
		id:                   id,
		defaultHashType:      defaultHashType,
		uiPrinter:            uiPrinter,
		buckets:              defaultBuckets,
		config:               config,
		blobCache:            make(map[string]struct{}),
		sshClientInitializer: sshClientInitializer,
		observer:             observer,
	}

	return blobStore, err
}

func (blobStore *remoteSftp) GetBlobStoreConfig() blob_store_configs.Config {
	// Per ADR 0005 / issue #60: the authoritative blob-store-properties
	// config is the one decoded from the remote `blob_store-config`
	// file, not the local SFTP transport config. Force initialization
	// so remoteConfig is populated; the local transport remains
	// reachable via BlobStoreInitialized.Config when callers need it.
	blobStore.initializeOnce()
	return blobStore.remoteConfig
}

func (blobStore *remoteSftp) GetDefaultHashType() domain_interfaces.FormatHash {
	// defaultHashType is overwritten by readRemoteConfig during lazy
	// init; without this call the constructor default (sha256 from
	// blob_store_configs.DefaultHashTypeId) is returned and callers
	// like Write.doOne -check compute the file digest with the wrong
	// hash. Matches the convention used by HasBlob, AllBlobs,
	// MakeBlobWriter, MakeBlobReader, and GetBlobIOWrapper.
	blobStore.initializeOnce()
	return blobStore.defaultHashType
}

func (blobStore *remoteSftp) close() (err error) {
	if blobStore.sftpClient != nil {
		if err = blobStore.sftpClient.Close(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return nil
}

func (blobStore *remoteSftp) initializeOnce() {
	blobStore.once.Do(func() {
		if err := blobStore.initialize(); err != nil {
			err = errors.Wrap(err)
			blobStore.ctx.Cancel(err)
		}
	})
}

func (blobStore *remoteSftp) readRemoteConfig() (err error) {
	remotePath := blobStore.config.GetRemotePath()
	configPath := path.Join(remotePath, directory_layout.FileNameBlobStoreConfig)

	blobStore.uiPrinter.Printf("reading remote config %q...", configPath)

	var configFile *sftp.File

	if configFile, err = blobStore.sftpClient.Open(configPath); err != nil {
		if os.IsNotExist(err) {
			err = errors.Errorf(
				"remote blob store config missing at %q; "+
					"initialize the remote store or use --discover to infer config",
				configPath,
			)
		} else {
			err = errors.Wrapf(err, "failed to open remote blob store config")
		}

		return err
	}

	defer configFile.Close() //defer:err-checked

	var typedConfig hyphence.TypedBlob[blob_store_configs.Config]

	if _, err = blob_store_configs.Coder.DecodeFrom(
		&typedConfig,
		configFile,
	); err != nil {
		err = errors.Wrapf(err, "failed to decode remote blob store config at %q", configPath)
		return err
	}

	remoteConfig := typedConfig.Blob

	// Persist the decoded remote config so GetBlobStoreConfig can
	// return the authoritative blob-store-properties object per ADR
	// 0005 / issue #60. The hash-type and bucket extractions below
	// remain — they pre-cache fields the hot path uses without going
	// through the interface assertions every call.
	blobStore.remoteConfig = remoteConfig

	if hashTypeConfig, ok := remoteConfig.(blob_store_configs.ConfigHashType); ok {
		blobStore.multiHash = hashTypeConfig.SupportsMultiHash()

		if blobStore.defaultHashType, err = markl.GetFormatHashOrError(
			hashTypeConfig.GetDefaultHashTypeId(),
		); err != nil {
			err = errors.Wrapf(err, "remote config has unsupported hash type")
			return err
		}
	} else {
		err = errors.Errorf(
			"remote blob store config type %T does not provide hash type information",
			remoteConfig,
		)
		return err
	}

	if bucketConfig, ok := remoteConfig.(blob_store_configs.ConfigLocalHashBucketed); ok {
		blobStore.buckets = bucketConfig.GetHashBuckets()
	} else {
		err = errors.Errorf(
			"remote blob store config type %T does not provide hash bucket information",
			remoteConfig,
		)
		return err
	}

	if ioWrapper, ok := remoteConfig.(domain_interfaces.BlobIOWrapper); ok {
		blobStore.blobIOWrapper = ioWrapper
	} else {
		err = errors.Errorf(
			"remote blob store config type %T does not provide blob IO wrapper information",
			remoteConfig,
		)
		return err
	}

	blobStore.uiPrinter.Printf(
		"remote config: hash=%s buckets=%v multi-hash=%t",
		blobStore.defaultHashType.GetMarklFormatId(),
		blobStore.buckets,
		blobStore.multiHash,
	)

	return err
}

func (blobStore *remoteSftp) initialize() (err error) {
	if blobStore.sshClient, err = blobStore.sshClientInitializer(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if blobStore.sftpClient, err = sftp.NewClient(blobStore.sshClient); err != nil {
		err = errors.Wrapf(err, "failed to create SFTP client")
		return err
	}

	blobStore.ctx.After(errors.MakeFuncContextFromFuncErr(blobStore.close))

	if err = blobStore.readRemoteConfig(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	remotePath := blobStore.config.GetRemotePath()

	// Create directory tree if it doesn't exist
	parts := strings.Split(remotePath, "/")
	var currentPath string

	for _, part := range parts {
		if part == "" {
			continue
		}

		if currentPath == "" && !strings.HasPrefix(remotePath, "/") {
			currentPath = part
		} else {
			currentPath = path.Join(currentPath, part)
		}

		blobStore.uiPrinter.Printf("checking directory %q...", currentPath)
		_, err = blobStore.sftpClient.Stat(currentPath)
		if err == nil {
			continue
		}
		if !errors.IsNotExist(err) {
			err = errors.Wrapf(err, "stat %q", currentPath)
			return err
		}

		blobStore.uiPrinter.Printf("creating directory %q...", currentPath)
		if err = blobStore.sftpClient.Mkdir(currentPath); err != nil {
			// Another client may have created the directory between
			// our Stat and Mkdir. Re-Stat to confirm existence before
			// treating the Mkdir error as benign.
			if _, statErr := blobStore.sftpClient.Stat(currentPath); statErr != nil {
				err = errors.Wrapf(
					err,
					"mkdir %q failed and path still missing",
					currentPath,
				)
				return err
			}
			err = nil
		}
	}

	return err
}

func (blobStore *remoteSftp) GetBlobStoreDescription() string {
	return "remote sftp hash bucketed"
}

func (blobStore *remoteSftp) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	blobStore.initializeOnce()
	return blobStore.blobIOWrapper
}

func (blobStore *remoteSftp) GetLocalBlobStore() domain_interfaces.BlobStore {
	return blobStore
}

func (blobStore *remoteSftp) makeEnvDirConfig() env_dir.Config {
	return env_dir.MakeConfig(
		blobStore.defaultHashType,
		env_dir.MakeHashBucketPathJoinFunc(blobStore.buckets),
		blobStore.blobIOWrapper.GetBlobCompression(),
		blobStore.blobIOWrapper.GetBlobEncryption(),
	)
}

func (blobStore *remoteSftp) remotePathForMerkleId(
	merkleId domain_interfaces.MarklId,
) string {
	return env_dir.MakeHashBucketPathFromMerkleId(
		merkleId,
		blobStore.buckets,
		blobStore.multiHash,
		strings.TrimPrefix(blobStore.config.GetRemotePath(), "/"),
	)
}

func (blobStore *remoteSftp) HasBlob(
	merkleId domain_interfaces.MarklId,
) (ok bool) {
	blobStore.initializeOnce()

	if merkleId.IsNull() {
		ok = true
		return ok
	}

	blobStore.blobCacheLock.RLock()

	if _, ok = blobStore.blobCache[string(merkleId.GetBytes())]; ok {
		blobStore.blobCacheLock.RUnlock()
		return ok
	}

	blobStore.blobCacheLock.RUnlock()

	remotePath := blobStore.remotePathForMerkleId(merkleId)

	if _, err := blobStore.sftpClient.Stat(remotePath); err == nil {
		blobStore.blobCacheLock.Lock()
		blobStore.blobCache[string(merkleId.GetBytes())] = struct{}{}
		blobStore.blobCacheLock.Unlock()
		ok = true
	}

	return ok
}

func (blobStore *remoteSftp) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	blobStore.initializeOnce()

	return func(yield func(domain_interfaces.MarklId, error) bool) {
		basePath := strings.TrimPrefix(blobStore.config.GetRemotePath(), "/")

		// Walk through the two-level directory structure (Git-like bucketing)
		walker := blobStore.sftpClient.Walk(basePath)

		digest, repool := blobStore.defaultHashType.GetBlobId()
		defer repool()

		for walker.Step() {
			if err := walker.Err(); err != nil {
				if !yield(nil, errors.Wrapf(err, "BasePath: %q", basePath)) {
					return
				}

				continue
			}

			info := walker.Stat()

			if info.IsDir() {
				continue
			}

			currentPath := walker.Path()

			{
				var err error

				if currentPath, err = filepath.Rel(basePath, currentPath); err != nil {
					if !yield(
						nil,
						errors.Wrapf(err, "BasePath: %q", basePath),
					) {
						return
					}
				}
			}

			if err := markl.SetHexStringFromAbsolutePath(
				digest,
				currentPath,
				basePath,
			); err != nil {
				if !yield(nil, errors.Wrap(err)) {
					return
				}

				continue
			}

			blobStore.blobCacheLock.Lock()
			blobStore.blobCache[string(digest.GetBytes())] = struct{}{}
			blobStore.blobCacheLock.Unlock()

			if !yield(digest, nil) {
				return
			}
		}
	}
}

func (blobStore *remoteSftp) MakeBlobWriter(
	marklHashType domain_interfaces.FormatHash,
) (blobWriter domain_interfaces.BlobWriter, err error) {
	blobStore.initializeOnce()

	// TODO use hash type
	mover := &sftpMover{
		store:  blobStore,
		config: blobStore.makeEnvDirConfig(),
	}

	hash, _ := blobStore.defaultHashType.Get() //repool:owned

	if err = mover.initialize(hash); err != nil {
		err = errors.Wrap(err)
		return blobWriter, err
	}

	blobWriter = mover

	return blobWriter, err
}

func (blobStore *remoteSftp) MakeBlobReader(
	digest domain_interfaces.MarklId,
) (readCloser domain_interfaces.BlobReader, err error) {
	blobStore.initializeOnce()

	if digest.IsNull() {
		hash, _ := blobStore.defaultHashType.Get() //repool:owned
		readCloser = markl_io.MakeNopReadCloser(
			hash,
			ohio.NopCloser(bytes.NewReader(nil)),
		)
		return readCloser, err
	}

	remotePath := blobStore.remotePathForMerkleId(digest)

	var remoteFile *sftp.File

	if remoteFile, err = blobStore.sftpClient.Open(remotePath); err != nil {
		if os.IsNotExist(err) {
			clonedDigest, _ := markl.Clone(digest) //repool:owned
			err = env_dir.ErrBlobMissing{
				BlobId: clonedDigest,
				Path:   remotePath,
			}
		} else {
			err = errors.Wrap(err)
		}
		return readCloser, err
	}

	blobStore.blobCacheLock.Lock()
	blobStore.blobCache[string(digest.GetBytes())] = struct{}{}
	blobStore.blobCacheLock.Unlock()

	// Create streaming reader that handles decompression/decryption
	config := blobStore.makeEnvDirConfig()
	streamingReader := &sftpStreamingReader{
		file:   remoteFile,
		config: config,
	}

	readerHash, _ := blobStore.defaultHashType.Get() //repool:owned

	if readCloser, err = streamingReader.createReader(
		readerHash,
	); err != nil {
		remoteFile.Close()
		err = errors.Wrap(err)
		return readCloser, err
	}

	return readCloser, err
}

// sftpMover implements interfaces.Mover and interfaces.ShaWriteCloser
// TODO explore using env_dir.Mover generically instead of this
type sftpMover struct {
	hash     domain_interfaces.Hash
	store    *remoteSftp
	config   env_dir.Config
	tempFile *sftp.File
	tempPath string
	writer   *sftpWriter
	closed   bool

	// bytesWritten counts the pre-compression/encryption bytes the
	// caller handed to Write/ReadFrom. Reported as the observer
	// event's Size field on successful upload. Pre-compression is
	// deliberately inconsistent with localFileMover (which stats the
	// on-disk file and thus reports the compressed size) — the
	// follow-up bats SFTP test rig can tighten this up by stat'ing
	// the remote blob instead.
	bytesWritten int64
}

// emitWriteEvent surfaces a BlobWriteEvent to the store's observer
// when one is wired. Nil observer (write-log disabled, or store
// constructed before the observer was attached) is a clean no-op.
// MarklId is pulled from the mover's current digest state; callers
// are responsible for only invoking this after the writer has been
// closed so the digest is stable.
func (mover *sftpMover) emitWriteEvent(
	op domain_interfaces.BlobWriteOp,
	size int64,
) {
	if mover.store == nil || mover.store.observer == nil {
		return
	}

	var markl domain_interfaces.MarklId
	if mover.writer != nil {
		markl = mover.writer.GetDigest()
	}

	mover.store.observer.OnBlobPublished(
		domain_interfaces.BlobWriteEvent{
			StoreId: mover.store.id.String(),
			MarklId: markl,
			Size:    size,
			Op:      op,
		},
	)
}

func (mover *sftpMover) initialize(hash domain_interfaces.Hash) (err error) {
	mover.hash = hash

	// Create a temporary file on the remote server
	var tempNameBytes [16]byte
	if _, err = rand.Read(tempNameBytes[:]); err != nil {
		err = errors.Wrap(err)
		return err
	}

	tempName := fmt.Sprintf("tmp_%x", tempNameBytes)
	mover.tempPath = path.Join(mover.store.config.GetRemotePath(), tempName)

	if mover.tempFile, err = mover.store.sftpClient.Create(
		mover.tempPath,
	); err != nil {
		ui.Debug().Printf("unable to create temp file: %q", mover.tempPath)
		err = errors.Wrapf(err, "unable to create temp file: %q", mover.tempPath)
		return err
	}

	// Create the streaming writer with compression/encryption

	if mover.writer, err = newSftpWriter(
		mover.config,
		mover.tempFile,
		hash,
	); err != nil {
		return errors.Join(
			errors.Wrap(err),
			mover.tempFile.Close(),
			mover.store.sftpClient.Remove(mover.tempPath),
		)
	}

	return err
}

func (mover *sftpMover) Write(p []byte) (n int, err error) {
	if mover.writer == nil {
		err = errors.ErrorWithStackf("writer not initialized")
		return n, err
	}

	n, err = mover.writer.Write(p)
	mover.bytesWritten += int64(n)
	return n, err
}

func (mover *sftpMover) ReadFrom(r io.Reader) (n int64, err error) {
	if mover.writer == nil {
		err = errors.ErrorWithStackf("writer not initialized")
		return n, err
	}

	n, err = mover.writer.ReadFrom(r)
	mover.bytesWritten += n
	return n, err
}

func (mover *sftpMover) Close() (err error) {
	if mover.closed {
		return nil
	}

	mover.closed = true

	// Deferred cleanup joins its errors into err so temp-file or
	// temp-path leaks on the remote are not silent. mover.tempFile and
	// mover.tempPath are niled out as they are consumed in the success
	// paths below so the defer becomes a no-op and we do not double-
	// close or attempt to remove a renamed file.
	defer func() {
		var cerr, rerr error
		if mover.tempFile != nil {
			cerr = mover.tempFile.Close()
		}
		if mover.tempPath != "" {
			rerr = mover.store.sftpClient.Remove(mover.tempPath)
		}
		if joined := errors.Join(err, cerr, rerr); joined != nil {
			err = joined
		}
	}()

	if mover.writer == nil {
		// No data was written
		return nil
	}

	// Close the writer to finalize compression/encryption and digest
	// calculation
	if err = mover.writer.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// Close the temp file
	if err = mover.tempFile.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}
	mover.tempFile = nil

	// Get the calculated digest and determine final path
	blobDigest := mover.writer.GetDigest()
	finalPath := mover.store.remotePathForMerkleId(blobDigest)

	// Ensure the target directory exists (Git-like bucketing)
	finalDir := path.Dir(finalPath)
	if err = mover.store.sftpClient.MkdirAll(finalDir); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// Atomically move temp file to final location
	if err = mover.store.sftpClient.Rename(mover.tempPath, finalPath); err != nil {
		// Check if file already exists
		if _, statErr := mover.store.sftpClient.Stat(finalPath); statErr == nil {
			// File already exists, this is OK — leave tempPath set so
			// the deferred cleanup removes the temp file and surfaces
			// any error from that removal.
			err = nil
		} else {
			err = errors.Wrap(err)
			return err
		}
	} else {
		// Rename succeeded: the temp file is already gone, don't let
		// the deferred cleanup try to remove it.
		mover.tempPath = ""

		// Per ADR 0004 / issue #50: emit the audit event only for the
		// cleanly-written case. The already-exists branch above reaches
		// the same cache update but its disposition is tracked as a
		// follow-up (see commit body).
		mover.emitWriteEvent(
			domain_interfaces.BlobWriteOpWritten,
			mover.bytesWritten,
		)
	}

	mover.store.blobCacheLock.Lock()
	mover.store.blobCache[string(blobDigest.GetBytes())] = struct{}{}
	mover.store.blobCacheLock.Unlock()

	return err
}

func (mover *sftpMover) GetMarklId() domain_interfaces.MarklId {
	if mover.writer == nil {
		return mover.GetMarklId()
	}

	return mover.writer.GetDigest()
}

// sftpWriter implements the streaming writer with compression/encryption
type sftpWriter struct {
	hash            domain_interfaces.Hash
	tee             io.Writer
	wCompress, wAge io.WriteCloser
	wBuf            *bufio.Writer
}

func newSftpWriter(
	config env_dir.Config,
	ioWriter io.Writer,
	hash domain_interfaces.Hash,
) (writer *sftpWriter, err error) {
	writer = &sftpWriter{}

	writer.wBuf = bufio.NewWriter(ioWriter)

	if writer.wAge, err = config.GetBlobEncryption().WrapWriter(writer.wBuf); err != nil {
		err = errors.Wrap(err)
		return writer, err
	}

	writer.hash = hash

	if writer.wCompress, err = config.GetBlobCompression().WrapWriter(writer.wAge); err != nil {
		err = errors.Wrap(err)
		return writer, err
	}

	writer.tee = io.MultiWriter(writer.hash, writer.wCompress)

	return writer, err
}

func (writer *sftpWriter) ReadFrom(r io.Reader) (n int64, err error) {
	if n, err = io.Copy(writer.tee, r); err != nil {
		err = errors.Wrap(err)
		return n, err
	}

	return n, err
}

func (writer *sftpWriter) Write(p []byte) (n int, err error) {
	return writer.tee.Write(p)
}

func (writer *sftpWriter) Close() (err error) {
	if writer.wCompress != nil {
		if err = writer.wCompress.Close(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	if writer.wAge != nil {
		if err = writer.wAge.Close(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	if writer.wBuf != nil {
		if err = writer.wBuf.Flush(); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}

func (writer *sftpWriter) GetDigest() domain_interfaces.MarklId {
	id, _ := writer.hash.GetMarklId() //repool:owned
	return id
}

// sftpStreamingReader handles decompression/decryption while reading from SFTP
// TODO combine with sftpReader
type sftpStreamingReader struct {
	file   *sftp.File
	config env_dir.Config
}

func (reader *sftpStreamingReader) createReader(
	hash domain_interfaces.Hash,
) (readCloser domain_interfaces.BlobReader, err error) {
	// Create streaming reader with decompression/decryption
	sftpReader := &sftpReader{
		file:   reader.file,
		config: reader.config,
	}

	if err = sftpReader.initialize(hash); err != nil {
		err = errors.Wrap(err)
		return readCloser, err
	}

	readCloser = sftpReader

	return readCloser, err
}

// sftpReader implements streaming decompression/decryption for SFTP
type sftpReader struct {
	file      *sftp.File
	config    env_dir.Config
	hash      domain_interfaces.Hash
	decrypter io.Reader
	expander  io.ReadCloser
	tee       io.Reader
}

func (reader *sftpReader) initialize(hash domain_interfaces.Hash) (err error) {
	// Set up decryption
	if reader.decrypter, err = reader.config.GetBlobEncryption().WrapReader(reader.file); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// Set up decompression
	if reader.expander, err = reader.config.GetBlobCompression().WrapReader(reader.decrypter); err != nil {
		err = errors.Wrap(err)
		return err
	}

	reader.hash = hash
	reader.tee = io.TeeReader(reader.expander, reader.hash)

	return err
}

func (reader *sftpReader) Read(p []byte) (n int, err error) {
	return reader.tee.Read(p)
}

func (reader *sftpReader) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, reader.tee)
}

func (reader *sftpReader) Seek(
	offset int64,
	whence int,
) (actual int64, err error) {
	seeker, ok := reader.decrypter.(io.Seeker)

	if !ok {
		err = errors.ErrorWithStackf("seeking not supported")
		return actual, err
	}

	return seeker.Seek(offset, whence)
}

func (reader *sftpReader) ReadAt(p []byte, off int64) (n int, err error) {
	readerAt, ok := reader.decrypter.(io.ReaderAt)

	if !ok {
		err = errors.ErrorWithStackf("reading at not supported")
		return n, err
	}

	return readerAt.ReadAt(p, off)
}

func (reader *sftpReader) Close() error {
	return errors.Join(
		reader.expander.Close(),
		reader.file.Close(),
	)
}

func (reader *sftpReader) GetMarklId() domain_interfaces.MarklId {
	id, _ := reader.hash.GetMarklId() //repool:owned
	return id
}
