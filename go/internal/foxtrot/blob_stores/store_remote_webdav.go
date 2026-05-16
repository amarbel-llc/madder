package blob_stores

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/blob_store_id"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/charlie/hyphence"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/purse-first/libs/dewey/0/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ohio"
	"github.com/amarbel-llc/purse-first/libs/dewey/charlie/ui"
)

type remoteWebdav struct {
	ctx       interfaces.ActiveContext
	uiPrinter ui.Printer
	once      sync.Once

	id blob_store_id.Id

	buckets []int

	config  blob_store_configs.ConfigWebDAV
	baseURL *url.URL

	// remoteConfig is the authoritative blob-store-properties config
	// decoded from the WebDAV remote's blob_store-config file. Per ADR
	// 0005, the local `config` (TomlWebDAVV0) above is transport only;
	// hash type, buckets, compression, and encryption all live here.
	// Populated by readRemoteConfig; nil before initializeOnce runs.
	remoteConfig blob_store_configs.Config

	multiHash       bool
	defaultHashType markl.FormatHash

	// blobIOWrapper holds the remote config's compression / encryption
	// view per ADR 0005. Populated by readRemoteConfig; nil before
	// initializeOnce runs.
	blobIOWrapper        domain_interfaces.BlobIOWrapper
	httpClientInitializer func() (*http.Client, error)
	httpClient            *http.Client

	// initErr is the sticky error captured by initializeOnce when
	// initialize() fails. Cached here so that subsequent
	// initializeOnce calls (sync.Once does not re-run f after a
	// panic) can re-panic the same error rather than silently
	// proceeding against a half-initialized struct. Mirrors SFTP's
	// initErr handling per the issue #134 lessons.
	initErr error

	// observer receives one BlobWriteEvent per successful upload. Set
	// at store-construction time from envDir.GetBlobWriteObserver().
	// Nil means no audit logging; the mover's emitWriteEvent handles
	// that case cleanly.
	observer domain_interfaces.BlobWriteObserver

	blobCacheLock sync.RWMutex
	blobCache     map[string]struct{}

	// mkcolledPaths caches URLs we've successfully MKCOL'd (or
	// confirmed-as-collection) so subsequent writes don't pay
	// MKCOL+PROPFIND round-trips for parent buckets that already
	// exist. SFTP gets this for free via local-state-aware MkdirAll;
	// WebDAV doesn't, so we cache explicitly. sync.Map is sufficient
	// because the only operations are load and store.
	mkcolledPaths sync.Map
}

var _ domain_interfaces.BlobStore = &remoteWebdav{}

func makeWebdavStore(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	id blob_store_id.Id,
	config blob_store_configs.ConfigWebDAV,
	httpClientInitializer func() (*http.Client, error),
	observer domain_interfaces.BlobWriteObserver,
) (blobStore *remoteWebdav, err error) {
	if err = validateWebdavAuth(config); err != nil {
		err = errors.Wrap(err)
		return blobStore, err
	}

	var defaultHashType markl.FormatHash

	if defaultHashType, err = markl.GetFormatHashOrError(
		blob_store_configs.DefaultHashTypeId,
	); err != nil {
		err = errors.Wrap(err)
		return blobStore, err
	}

	rawURL := config.GetURL()
	// Strip trailing slash for internal storage; we re-append it when
	// building child URLs. Normalizing once at construction time means
	// callers don't have to think about it.
	rawURL = strings.TrimRight(rawURL, "/")

	var baseURL *url.URL
	if baseURL, err = url.Parse(rawURL); err != nil {
		err = errors.Wrapf(err, "failed to parse WebDAV URL %q", config.GetURL())
		return blobStore, err
	}

	blobStore = &remoteWebdav{
		ctx:                   ctx,
		id:                    id,
		defaultHashType:       defaultHashType,
		uiPrinter:             uiPrinter,
		buckets:               defaultBuckets,
		config:                config,
		baseURL:               baseURL,
		blobCache:             make(map[string]struct{}),
		httpClientInitializer: httpClientInitializer,
		observer:              observer,
	}

	return blobStore, err
}

func (blobStore *remoteWebdav) GetBlobStoreConfig() blob_store_configs.Config {
	// Per ADR 0005: the authoritative blob-store-properties config is
	// the one decoded from the remote `blob_store-config` file, not
	// the local WebDAV transport config.
	blobStore.initializeOnce()
	return blobStore.remoteConfig
}

func (blobStore *remoteWebdav) GetDefaultHashType() domain_interfaces.FormatHash {
	blobStore.initializeOnce()
	return blobStore.defaultHashType
}

// initializeOnce lazily connects to the remote WebDAV server and reads
// the remote blob-store-properties config. Mirrors remoteSftp's
// initializeOnce: on failure it panics with the wrapped error;
// sync.Once.Do does not re-run f after a prior panic, so subsequent
// callers re-panic the cached initErr rather than proceeding against
// a half-initialized struct.
func (blobStore *remoteWebdav) initializeOnce() {
	blobStore.once.Do(func() {
		if err := blobStore.initialize(); err != nil {
			blobStore.initErr = errors.Wrap(err)
		}
	})
	if blobStore.initErr != nil {
		panic(blobStore.initErr)
	}
}

func (blobStore *remoteWebdav) close() error {
	if blobStore.httpClient != nil {
		blobStore.httpClient.CloseIdleConnections()
	}
	return nil
}

func (blobStore *remoteWebdav) initialize() (err error) {
	if blobStore.httpClient, err = blobStore.httpClientInitializer(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	blobStore.ctx.After(errors.MakeFuncContextFromFuncErr(blobStore.close))

	if err = blobStore.readRemoteConfig(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	// Ensure the base path exists. WebDAV servers vary in whether
	// PUT-creates-parents is supported; we MKCOL the base URL up front
	// so writes don't need to. 405 means "already exists"; we
	// double-check it's a collection (not a file) via PROPFIND
	// Depth:0.
	if err = blobStore.ensureCollection(blobStore.baseURL.String()); err != nil {
		err = errors.Wrap(err)
		return err
	}

	return err
}

func (blobStore *remoteWebdav) readRemoteConfig() (err error) {
	configURL := blobStore.baseURL.String() + "/" + directory_layout.FileNameBlobStoreConfig

	blobStore.uiPrinter.Printf("reading remote config %q...", configURL)

	req, err := http.NewRequestWithContext(blobStore.ctx, http.MethodGet, configURL, nil)
	if err != nil {
		err = errors.Wrap(err)
		return err
	}
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		err = errors.Wrapf(err, "failed to GET remote blob store config")
		return err
	}
	defer resp.Body.Close() //defer:err-checked

	if resp.StatusCode == http.StatusNotFound {
		err = errors.Errorf(
			"remote blob store config missing at %q; "+
				"initialize the remote store",
			configURL,
		)
		return err
	}
	if resp.StatusCode != http.StatusOK {
		err = errors.Errorf(
			"unexpected status %d reading remote config at %q",
			resp.StatusCode, configURL,
		)
		return err
	}

	var typedConfig hyphence.TypedBlob[blob_store_configs.Config]

	if _, err = blob_store_configs.Coder.DecodeFrom(
		&typedConfig,
		resp.Body,
	); err != nil {
		err = errors.Wrapf(err, "failed to decode remote blob store config at %q", configURL)
		return err
	}

	remoteConfig := typedConfig.Blob
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

func (blobStore *remoteWebdav) GetBlobStoreDescription() string {
	return "remote webdav hash bucketed"
}

func (blobStore *remoteWebdav) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	blobStore.initializeOnce()
	return blobStore.blobIOWrapper
}

func (blobStore *remoteWebdav) GetLocalBlobStore() domain_interfaces.BlobStore {
	return blobStore
}

func (blobStore *remoteWebdav) makeEnvDirConfig() blob_io.Config {
	return blob_io.MakeConfig(
		blobStore.defaultHashType,
		blob_io.MakeHashBucketPathJoinFunc(blobStore.buckets),
		blobStore.blobIOWrapper.GetBlobCompression(),
		blobStore.blobIOWrapper.GetBlobEncryption(),
	)
}

func (blobStore *remoteWebdav) setAuth(req *http.Request) {
	applyWebdavAuth(req, blobStore.config)
}

func (blobStore *remoteWebdav) urlForRelPath(relPath string) string {
	if relPath == "" {
		return blobStore.baseURL.String()
	}
	return blobStore.baseURL.String() + "/" + strings.TrimLeft(relPath, "/")
}

func (blobStore *remoteWebdav) urlForMerkleId(
	merkleId domain_interfaces.MarklId,
) string {
	relPath := blob_io.MakeHashBucketPathFromMerkleId(
		merkleId,
		blobStore.buckets,
		blobStore.multiHash,
		"",
	)
	return blobStore.urlForRelPath(relPath)
}

func (blobStore *remoteWebdav) HasBlob(
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

	blobURL := blobStore.urlForMerkleId(merkleId)

	req, err := http.NewRequestWithContext(blobStore.ctx, http.MethodHead, blobURL, nil)
	if err != nil {
		return false
	}
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusOK {
		blobStore.blobCacheLock.Lock()
		blobStore.blobCache[string(merkleId.GetBytes())] = struct{}{}
		blobStore.blobCacheLock.Unlock()
		ok = true
	}

	return ok
}

func (blobStore *remoteWebdav) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	blobStore.initializeOnce()

	if blobStore.multiHash {
		return blobStore.allBlobsMultiHash()
	}
	return blobStore.allBlobsForBase("", blobStore.defaultHashType)
}

// allBlobsMultiHash mirrors remoteSftp.allBlobsMultiHash: walks each
// `<basePath>/<format-id>/` subtree, picking the matching hash type
// per subtree, so reconstructed ids don't carry the format-id segment.
func (blobStore *remoteWebdav) allBlobsMultiHash() interfaces.SeqError[domain_interfaces.MarklId] {
	return func(yield func(domain_interfaces.MarklId, error) bool) {
		responses, err := blobStore.propfind(blobStore.baseURL.String(), depthOne)
		if err != nil {
			yield(nil, errors.Wrap(err))
			return
		}

		baseHref := blobStore.basePath()

		for _, resp := range responses {
			// PROPFIND Depth:1 includes the base resource itself in
			// the response list; skip it explicitly so we don't try
			// to parse the base path's leaf name as a hash format.
			if strings.TrimRight(resp.Href, "/") == baseHref {
				continue
			}

			if !resp.IsCollection {
				continue
			}

			name := pathLeafName(resp.Href)
			if name == "" || name == "." {
				continue
			}

			hashType, err := markl.GetFormatHashOrError(name)
			if err != nil {
				if !yield(nil, errors.Wrap(err)) {
					return
				}
				continue
			}

			for id, err := range blobStore.allBlobsForBase(name, hashType) {
				if !yield(id, err) {
					return
				}
			}
		}
	}
}

// allBlobsForBase walks `basePath` (relative to baseURL) via PROPFIND
// Depth: infinity, yielding a markl id reconstructed from each leaf
// path. The PROPFIND walker filters out `blob_store-config` and
// `tmp_*` upload artifacts (mirrors SFTP's shouldSkipBlobWalkEntry).
func (blobStore *remoteWebdav) allBlobsForBase(
	basePath string,
	hashType markl.FormatHash,
) interfaces.SeqError[domain_interfaces.MarklId] {
	return func(yield func(domain_interfaces.MarklId, error) bool) {
		url := blobStore.urlForRelPath(basePath)

		responses, err := blobStore.propfind(url, depthInfinity)
		if err != nil {
			if !yield(nil, errors.Wrapf(err, "BasePath: %q", basePath)) {
				return
			}
			return
		}

		digest, repool := hashType.GetBlobId()
		defer repool()

		baseHref := blobStore.basePathForRel(basePath)

		for _, resp := range responses {
			if resp.IsCollection {
				continue
			}

			href := strings.TrimRight(resp.Href, "/")
			// `relPath` is the bucket/leaf chain relative to baseHref.
			relPath := strings.TrimPrefix(href, baseHref)
			relPath = strings.TrimLeft(relPath, "/")
			if relPath == "" {
				continue
			}

			if shouldSkipBlobWalkEntry(path.Base(relPath)) {
				continue
			}

			if err := markl.SetHexStringFromRelPath(digest, relPath); err != nil {
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

func (blobStore *remoteWebdav) basePath() string {
	return strings.TrimRight(blobStore.baseURL.Path, "/")
}

func (blobStore *remoteWebdav) basePathForRel(rel string) string {
	if rel == "" {
		return blobStore.basePath()
	}
	return blobStore.basePath() + "/" + strings.TrimLeft(rel, "/")
}

func pathLeafName(href string) string {
	href = strings.TrimRight(href, "/")
	i := strings.LastIndex(href, "/")
	if i < 0 {
		return href
	}
	return href[i+1:]
}

func (blobStore *remoteWebdav) MakeBlobWriter(
	marklHashType domain_interfaces.FormatHash,
) (blobWriter domain_interfaces.BlobWriter, err error) {
	blobStore.initializeOnce()

	// TODO use hash type
	mover := &webdavMover{
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

func (blobStore *remoteWebdav) MakeBlobReader(
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

	blobURL := blobStore.urlForMerkleId(digest)

	req, err := http.NewRequestWithContext(blobStore.ctx, http.MethodGet, blobURL, nil)
	if err != nil {
		err = errors.Wrap(err)
		return readCloser, err
	}
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		err = errors.Wrap(err)
		return readCloser, err
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close() //nolint:errcheck
		clonedDigest, _ := markl.Clone(digest) //repool:owned
		err = blob_io.ErrBlobMissing{
			BlobId: clonedDigest,
			Path:   blobURL,
		}
		return readCloser, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() //nolint:errcheck
		err = errors.Errorf(
			"unexpected status %d reading blob at %q",
			resp.StatusCode, blobURL,
		)
		return readCloser, err
	}

	blobStore.blobCacheLock.Lock()
	blobStore.blobCache[string(digest.GetBytes())] = struct{}{}
	blobStore.blobCacheLock.Unlock()

	config := blobStore.makeEnvDirConfig()
	reader := &webdavReader{
		body:   resp.Body,
		config: config,
	}

	readerHash, _ := blobStore.defaultHashType.Get() //repool:owned

	if err = reader.initialize(readerHash); err != nil {
		resp.Body.Close() //nolint:errcheck
		err = errors.Wrap(err)
		return readCloser, err
	}

	readCloser = reader
	return readCloser, err
}

// webdavMover handles the PUT-to-temp / MOVE-to-final dance with a
// HEAD-then-DELETE duplicate-write fallback on MOVE failure. Mirrors
// the SFTP mover at every layer; the only WebDAV-specific wrinkles
// are the HTTP method calls and the MKCOL-before-MOVE step that
// ensures the bucket path tree exists.
type webdavMover struct {
	hash         domain_interfaces.Hash
	store        *remoteWebdav
	config       blob_io.Config
	tempURL      string
	tempBuf      *bytes.Buffer
	writer       *webdavWriter
	closed       bool
	bytesWritten int64
}

func (mover *webdavMover) emitWriteEvent(
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

func (mover *webdavMover) initialize(hash domain_interfaces.Hash) (err error) {
	mover.hash = hash

	var tempNameBytes [16]byte
	if _, err = rand.Read(tempNameBytes[:]); err != nil {
		err = errors.Wrap(err)
		return err
	}
	tempName := fmt.Sprintf("tmp_%x", tempNameBytes)
	mover.tempURL = mover.store.urlForRelPath(tempName)
	mover.tempBuf = &bytes.Buffer{}

	if mover.writer, err = newWebdavWriter(
		mover.config,
		mover.tempBuf,
		hash,
	); err != nil {
		return errors.Wrap(err)
	}

	return err
}

func (mover *webdavMover) Write(p []byte) (n int, err error) {
	if mover.writer == nil {
		err = errors.ErrorWithStackf("writer not initialized")
		return n, err
	}
	n, err = mover.writer.Write(p)
	mover.bytesWritten += int64(n)
	return n, err
}

func (mover *webdavMover) ReadFrom(r io.Reader) (n int64, err error) {
	if mover.writer == nil {
		err = errors.ErrorWithStackf("writer not initialized")
		return n, err
	}
	n, err = mover.writer.ReadFrom(r)
	mover.bytesWritten += n
	return n, err
}

func (mover *webdavMover) Close() (err error) {
	if mover.closed {
		return nil
	}
	mover.closed = true

	// tempPosted guards the deferred DELETE: a temp URL we never PUT
	// would 404 on DELETE and surface as a spurious error.
	tempPosted := false
	defer func() {
		if mover.tempBuf != nil {
			mover.tempBuf.Reset()
			mover.tempBuf = nil
		}
		if !tempPosted {
			return
		}
		if rerr := mover.store.deleteResource(mover.tempURL); rerr != nil {
			if joined := errors.Join(err, rerr); joined != nil {
				err = joined
			}
		}
	}()

	if mover.writer == nil {
		return nil
	}

	if err = mover.writer.Close(); err != nil {
		err = errors.Wrap(err)
		return err
	}

	if err = mover.store.putResource(mover.tempURL, mover.tempBuf.Bytes()); err != nil {
		err = errors.Wrap(err)
		return err
	}
	tempPosted = true

	blobDigest := mover.writer.GetDigest()
	finalURL := mover.store.urlForMerkleId(blobDigest)

	// Ensure parent buckets exist. The hash-bucket path math
	// produces paths like `<bucket0>/<bucket1>/<leaf>`; we MKCOL
	// each parent in order. 405 = exists; validate it's a collection.
	if err = mover.store.ensureParentCollections(finalURL); err != nil {
		err = errors.Wrap(err)
		return err
	}

	moved, err := mover.store.moveResource(mover.tempURL, finalURL)
	if err != nil {
		return err
	}
	if moved {
		// MOVE succeeded — temp no longer exists at tempURL.
		tempPosted = false
		mover.emitWriteEvent(
			domain_interfaces.BlobWriteOpWritten,
			mover.bytesWritten,
		)
	}
	// If moved == false the blob already existed at finalURL; the
	// deferred DELETE will clean up the temp.

	mover.store.blobCacheLock.Lock()
	mover.store.blobCache[string(blobDigest.GetBytes())] = struct{}{}
	mover.store.blobCacheLock.Unlock()

	return err
}

func (mover *webdavMover) GetMarklId() domain_interfaces.MarklId {
	if mover.writer == nil {
		panic(errors.ErrorWithStackf(
			"webdavMover.GetMarklId called before initialize; mover.writer is nil",
		))
	}
	return mover.writer.GetDigest()
}

// webdavWriter is the compression/encryption pipeline atop a destination
// io.Writer (an in-memory buffer; the buffer's contents are PUT
// after Close). Identical shape to sftpWriter.
type webdavWriter struct {
	hash            domain_interfaces.Hash
	tee             io.Writer
	wCompress, wAge io.WriteCloser
	wBuf            *bufio.Writer
}

func newWebdavWriter(
	config blob_io.Config,
	ioWriter io.Writer,
	hash domain_interfaces.Hash,
) (writer *webdavWriter, err error) {
	writer = &webdavWriter{}
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

func (writer *webdavWriter) ReadFrom(r io.Reader) (n int64, err error) {
	if n, err = io.Copy(writer.tee, r); err != nil {
		err = errors.Wrap(err)
		return n, err
	}
	return n, err
}

func (writer *webdavWriter) Write(p []byte) (n int, err error) {
	return writer.tee.Write(p)
}

func (writer *webdavWriter) Close() (err error) {
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

func (writer *webdavWriter) GetDigest() domain_interfaces.MarklId {
	id, _ := writer.hash.GetMarklId() //repool:owned
	return id
}

// webdavReader mirrors sftpReader: streaming decompression/decryption
// atop a response body.
type webdavReader struct {
	body      io.ReadCloser
	config    blob_io.Config
	hash      domain_interfaces.Hash
	decrypter io.Reader
	expander  io.ReadCloser
	tee       io.Reader
}

func (reader *webdavReader) initialize(hash domain_interfaces.Hash) (err error) {
	if reader.decrypter, err = reader.config.GetBlobEncryption().WrapReader(reader.body); err != nil {
		err = errors.Wrap(err)
		return err
	}
	if reader.expander, err = reader.config.GetBlobCompression().WrapReader(reader.decrypter); err != nil {
		err = errors.Wrap(err)
		return err
	}
	reader.hash = hash
	reader.tee = io.TeeReader(reader.expander, reader.hash)
	return err
}

func (reader *webdavReader) Read(p []byte) (n int, err error) {
	return reader.tee.Read(p)
}

func (reader *webdavReader) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, reader.tee)
}

func (reader *webdavReader) Seek(offset int64, whence int) (actual int64, err error) {
	err = errors.ErrorWithStackf("seeking not supported")
	return actual, err
}

func (reader *webdavReader) ReadAt(p []byte, off int64) (n int, err error) {
	err = errors.ErrorWithStackf("reading at not supported")
	return n, err
}

func (reader *webdavReader) Close() error {
	return errors.Join(
		reader.expander.Close(),
		reader.body.Close(),
	)
}

func (reader *webdavReader) GetMarklId() domain_interfaces.MarklId {
	id, _ := reader.hash.GetMarklId() //repool:owned
	return id
}

// putResource issues a PUT to `url` with body `payload`. Treats any
// 2xx as success.
func (blobStore *remoteWebdav) putResource(url string, payload []byte) error {
	req, err := http.NewRequestWithContext(
		blobStore.ctx,
		http.MethodPut,
		url,
		bytes.NewReader(payload),
	)
	if err != nil {
		return errors.Wrap(err)
	}
	blobStore.setAuth(req)
	req.ContentLength = int64(len(payload))

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "PUT %q", url)
	}
	defer resp.Body.Close() //defer:err-checked

	if resp.StatusCode/100 != 2 {
		return errors.Errorf("PUT %q returned %d", url, resp.StatusCode)
	}
	return nil
}

// deleteResource issues a DELETE to `url`. 404 is treated as success.
func (blobStore *remoteWebdav) deleteResource(url string) error {
	req, err := http.NewRequestWithContext(blobStore.ctx, http.MethodDelete, url, nil)
	if err != nil {
		return errors.Wrap(err)
	}
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "DELETE %q", url)
	}
	defer resp.Body.Close() //defer:err-checked

	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return errors.Errorf("DELETE %q returned %d", url, resp.StatusCode)
}

// moveResource issues a MOVE with Overwrite: F. Returns
// (moved=true, nil) on success. On 412/409/507, HEADs `destURL`; if
// the destination exists, returns (moved=false, nil) so the caller
// treats this as a duplicate-write — never set Overwrite: T (would
// clobber a concurrent writer's blob and violate CAS).
func (blobStore *remoteWebdav) moveResource(srcURL, destURL string) (moved bool, err error) {
	req, err := http.NewRequestWithContext(blobStore.ctx, methodMove, srcURL, nil)
	if err != nil {
		return false, errors.Wrap(err)
	}
	req.Header.Set("Destination", destURL)
	req.Header.Set("Overwrite", "F")
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return false, errors.Wrapf(err, "MOVE %q -> %q", srcURL, destURL)
	}
	defer resp.Body.Close() //defer:err-checked

	if resp.StatusCode/100 == 2 {
		return true, nil
	}

	// Duplicate-write fallback per the design doc.
	switch resp.StatusCode {
	case http.StatusPreconditionFailed, http.StatusConflict, http.StatusInsufficientStorage:
		exists, headErr := blobStore.headExists(destURL)
		if headErr != nil {
			return false, errors.Wrapf(headErr, "HEAD %q after MOVE %d", destURL, resp.StatusCode)
		}
		if exists {
			return false, nil
		}
	}

	return false, errors.Errorf(
		"MOVE %q -> %q returned %d",
		srcURL, destURL, resp.StatusCode,
	)
}

func (blobStore *remoteWebdav) headExists(url string) (bool, error) {
	req, err := http.NewRequestWithContext(blobStore.ctx, http.MethodHead, url, nil)
	if err != nil {
		return false, errors.Wrap(err)
	}
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return false, errors.Wrap(err)
	}
	resp.Body.Close() //nolint:errcheck

	return resp.StatusCode == http.StatusOK, nil
}

// ensureCollection issues MKCOL at `url`; 405 means the resource
// exists, in which case we PROPFIND Depth:0 to confirm it's a
// collection (not a file). Returns nil if the resource exists as a
// collection at the end of the call. Caches successful results so
// hot bucket paths don't pay the round-trip on every write.
func (blobStore *remoteWebdav) ensureCollection(url string) error {
	if _, ok := blobStore.mkcolledPaths.Load(url); ok {
		return nil
	}

	req, err := http.NewRequestWithContext(blobStore.ctx, methodMkcol, url, nil)
	if err != nil {
		return errors.Wrap(err)
	}
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return errors.Wrapf(err, "MKCOL %q", url)
	}
	defer resp.Body.Close() //defer:err-checked

	if resp.StatusCode/100 == 2 {
		blobStore.mkcolledPaths.Store(url, struct{}{})
		return nil
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		return errors.Errorf("MKCOL %q returned %d", url, resp.StatusCode)
	}

	// 405 — confirm it's a collection, not a file.
	responses, err := blobStore.propfind(url, depthZero)
	if err != nil {
		return errors.Wrapf(err, "PROPFIND after MKCOL 405 at %q", url)
	}
	for _, r := range responses {
		if r.IsCollection {
			blobStore.mkcolledPaths.Store(url, struct{}{})
			return nil
		}
	}
	return errors.Errorf("MKCOL %q returned 405 but target is not a collection", url)
}

// ensureParentCollections walks the path components between baseURL
// and the final URL, MKCOLing each missing collection.
func (blobStore *remoteWebdav) ensureParentCollections(finalURL string) error {
	parsed, err := url.Parse(finalURL)
	if err != nil {
		return errors.Wrap(err)
	}
	basePath := blobStore.baseURL.Path
	finalPath := parsed.Path

	rel := strings.TrimPrefix(finalPath, basePath)
	rel = strings.TrimLeft(rel, "/")
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		// No parent directories beyond the base.
		return nil
	}

	currentURL := blobStore.baseURL.String()
	// MKCOL each parent in order, skipping the final filename.
	for _, part := range parts[:len(parts)-1] {
		if part == "" {
			continue
		}
		currentURL += "/" + part
		if err := blobStore.ensureCollection(currentURL); err != nil {
			return err
		}
	}
	return nil
}

// PROPFIND helpers below.

type webdavResponse struct {
	Href         string
	IsCollection bool
}

const (
	// WebDAV-specific HTTP methods (net/http only declares the
	// stdlib HTTP ones).
	methodMkcol    = "MKCOL"
	methodMove     = "MOVE"
	methodPropfind = "PROPFIND"

	depthZero     = "0"
	depthOne      = "1"
	depthInfinity = "infinity"
)

// propfind issues a PROPFIND at `url` with the given Depth header and
// returns a flat list of responses with the IsCollection bit
// extracted.
func (blobStore *remoteWebdav) propfind(url string, depth string) ([]webdavResponse, error) {
	const body = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:">
  <D:prop>
    <D:resourcetype/>
  </D:prop>
</D:propfind>`

	req, err := http.NewRequestWithContext(blobStore.ctx, methodPropfind, url, strings.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err)
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	blobStore.setAuth(req)

	resp, err := blobStore.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrapf(err, "PROPFIND %q (Depth: %s)", url, depth)
	}
	defer resp.Body.Close() //defer:err-checked

	if resp.StatusCode == http.StatusNotFound {
		// Drain the body so the underlying connection can be reused
		// from the keep-alive pool. Closing without draining drops
		// the connection.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil
	}
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, errors.Errorf(
			"PROPFIND %q returned %d", url, resp.StatusCode,
		)
	}

	var ms propfindMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, errors.Wrapf(err, "decode PROPFIND %q", url)
	}

	out := make([]webdavResponse, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		out = append(out, webdavResponse{
			Href:         r.Href,
			IsCollection: r.Prop.ResourceType.Collection != nil,
		})
	}
	return out, nil
}

type propfindMultistatus struct {
	XMLName   xml.Name           `xml:"DAV: multistatus"`
	Responses []propfindResponse `xml:"response"`
}

type propfindResponse struct {
	Href string       `xml:"href"`
	Prop propfindProp `xml:"propstat>prop"`
}

type propfindProp struct {
	ResourceType propfindResourceType `xml:"resourcetype"`
}

type propfindResourceType struct {
	Collection *struct{} `xml:"collection"`
}
