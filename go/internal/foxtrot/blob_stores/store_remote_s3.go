package blob_stores

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	stdpath "path"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/alfa/markl_io"
	"github.com/amarbel-llc/madder/go/internal/alfa/scoped_id"
	"github.com/amarbel-llc/madder/go/internal/bravo/directory_layout"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/madder/go/internal/foxtrot/blob_io"
	"github.com/amarbel-llc/piggy/go/markl/pkgs/markl"
	deweyerrors "github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/interfaces"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ohio"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/ui"
)

type remoteS3 struct {
	ctx       interfaces.ActiveContext
	uiPrinter ui.Printer
	once      sync.Once

	id scoped_id.Id

	buckets []int

	config blob_store_configs.ConfigS3

	// remoteConfig is the authoritative blob-store-properties config
	// decoded from the s3 object at <prefix>/blob_store-config (ADR 0005).
	// nil before initializeOnce runs.
	remoteConfig blob_store_configs.Config

	multiHash       bool
	defaultHashType markl.FormatHash

	blobIOWrapper domain_interfaces.BlobIOWrapper

	s3Client *s3.Client

	// initErr is the sticky error captured by initializeOnce when
	// initialize() fails. sync.Once doesn't re-run after a panic, so
	// cache the wrapped error and re-panic on each subsequent call
	// rather than proceeding with s3Client = nil.
	initErr error

	observer domain_interfaces.BlobWriteObserver

	blobCacheLock sync.RWMutex
	blobCache     map[string]struct{}
}

var _ domain_interfaces.BlobStore = &remoteS3{}

func makeS3Store(
	ctx interfaces.ActiveContext,
	uiPrinter ui.Printer,
	id scoped_id.Id,
	config blob_store_configs.ConfigS3,
	observer domain_interfaces.BlobWriteObserver,
) (blobStore *remoteS3, err error) {
	if config.GetBucket() == "" {
		err = deweyerrors.BadRequestf("s3 blob store config requires a bucket")
		return blobStore, err
	}

	if err = ValidateS3Auth(config); err != nil {
		err = deweyerrors.Wrap(err)
		return blobStore, err
	}

	var defaultHashType markl.FormatHash

	if defaultHashType, err = markl.GetFormatHashOrError(
		blob_store_configs.DefaultHashTypeId,
	); err != nil {
		err = deweyerrors.Wrap(err)
		return blobStore, err
	}

	blobStore = &remoteS3{
		ctx:             ctx,
		id:              id,
		defaultHashType: defaultHashType,
		uiPrinter:       uiPrinter,
		buckets:         defaultBuckets,
		config:          config,
		blobCache:       make(map[string]struct{}),
		observer:        observer,
	}

	return blobStore, err
}

// MakeS3Client builds a configured *s3.Client from a ConfigS3. Exported
// so the init-s3 command can write the remote blob_store-config object
// without standing up a full remoteS3 store.
func MakeS3Client(
	ctx context.Context,
	cfg blob_store_configs.ConfigS3,
) (*s3.Client, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{}

	if cfg.GetRegion() != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.GetRegion()))
	}

	if cfg.GetAccessKeyId() != "" {
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.GetAccessKeyId(),
				cfg.GetSecretAccessKey(),
				cfg.GetSessionToken(),
			),
		))
	}

	if cfg.GetInsecureSkipVerify() {
		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //#nosec G402 -- opt-in dev flag
			},
		}
		loadOpts = append(loadOpts, awsconfig.WithHTTPClient(httpClient))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, deweyerrors.Wrapf(err, "failed to load aws config")
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.GetEndpoint() != "" {
			o.BaseEndpoint = aws.String(cfg.GetEndpoint())
		}
		if cfg.GetUsePathStyle() {
			o.UsePathStyle = true
		}
	})

	return client, nil
}

func (blobStore *remoteS3) GetBlobStoreConfig() blob_store_configs.Config {
	blobStore.initializeOnce()
	return blobStore.remoteConfig
}

func (blobStore *remoteS3) GetDefaultHashType() domain_interfaces.FormatHash {
	blobStore.initializeOnce()
	return blobStore.defaultHashType
}

func (blobStore *remoteS3) initializeOnce() {
	blobStore.once.Do(func() {
		if err := blobStore.initialize(); err != nil {
			blobStore.initErr = deweyerrors.Wrap(err)
		}
	})
	if blobStore.initErr != nil {
		panic(blobStore.initErr)
	}
}

func (blobStore *remoteS3) initialize() (err error) {
	if blobStore.s3Client, err = MakeS3Client(blobStore.ctx, blobStore.config); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}

	if err = blobStore.readRemoteConfig(); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}

	return err
}

func (blobStore *remoteS3) configKey() string {
	return s3JoinKey(
		blobStore.config.GetPrefix(),
		directory_layout.FileNameBlobStoreConfig,
	)
}

func (blobStore *remoteS3) readRemoteConfig() (err error) {
	bucket := blobStore.config.GetBucket()
	key := blobStore.configKey()

	blobStore.uiPrinter.Printf("reading remote config s3://%s/%s ...", bucket, key)

	out, getErr := blobStore.s3Client.GetObject(blobStore.ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if getErr != nil {
		if isS3NotFound(getErr) {
			err = deweyerrors.Errorf(
				"remote blob store config missing at s3://%s/%s; "+
					"re-run init-s3 to bootstrap one",
				bucket, key,
			)
			return err
		}
		err = deweyerrors.Wrapf(getErr, "failed to get remote blob store config")
		return err
	}
	defer out.Body.Close() //defer:err-checked

	var typedConfig blob_store_configs.TypedConfig

	if _, err = blob_store_configs.DecodeAndVerify(&typedConfig, out.Body); err != nil {
		err = deweyerrors.Wrapf(err, "failed to decode remote blob store config at s3://%s/%s", bucket, key)
		return err
	}

	remoteConfig := typedConfig.Blob
	blobStore.remoteConfig = remoteConfig

	if hashTypeConfig, ok := remoteConfig.(blob_store_configs.ConfigHashType); ok {
		blobStore.multiHash = hashTypeConfig.SupportsMultiHash()

		if blobStore.defaultHashType, err = markl.GetFormatHashOrError(
			hashTypeConfig.GetDefaultHashTypeId(),
		); err != nil {
			err = deweyerrors.Wrapf(err, "remote config has unsupported hash type")
			return err
		}
	} else {
		err = deweyerrors.Errorf(
			"remote blob store config type %T does not provide hash type information",
			remoteConfig,
		)
		return err
	}

	if bucketConfig, ok := remoteConfig.(blob_store_configs.ConfigLocalHashBucketed); ok {
		blobStore.buckets = bucketConfig.GetHashBuckets()
	} else {
		err = deweyerrors.Errorf(
			"remote blob store config type %T does not provide hash bucket information",
			remoteConfig,
		)
		return err
	}

	if ioWrapper, ok := remoteConfig.(domain_interfaces.BlobIOWrapper); ok {
		blobStore.blobIOWrapper = ioWrapper
	} else {
		err = deweyerrors.Errorf(
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

func (blobStore *remoteS3) GetBlobStoreDescription() string {
	return fmt.Sprintf(
		"remote s3 hash bucketed (s3://%s/%s)",
		blobStore.config.GetBucket(),
		strings.TrimRight(blobStore.config.GetPrefix(), "/"),
	)
}

func (blobStore *remoteS3) GetBlobIOWrapper() domain_interfaces.BlobIOWrapper {
	blobStore.initializeOnce()
	return blobStore.blobIOWrapper
}

func (blobStore *remoteS3) GetLocalBlobStore() domain_interfaces.BlobStore {
	return blobStore
}

func (blobStore *remoteS3) makeEnvDirConfig() blob_io.Config {
	return blob_io.MakeConfig(
		blobStore.defaultHashType,
		blob_io.MakeHashBucketPathJoinFunc(blobStore.buckets),
		blobStore.blobIOWrapper.GetBlobCompression(),
		blobStore.blobIOWrapper.GetBlobEncryption(),
	)
}

// keyForMarklId returns the S3 object key for the given blob id, mirroring
// the local hash-bucketed layout: <prefix>/[<format-id>/]<aa>/<bb>/<rest>.
// Always uses forward-slash separators (S3 key convention) regardless of
// host OS.
func (blobStore *remoteS3) keyForMarklId(merkleId domain_interfaces.MarklId) string {
	hex := markl.FormatBytesAsHex(merkleId)
	parts := []string{}
	if p := strings.TrimRight(blobStore.config.GetPrefix(), "/"); p != "" {
		parts = append(parts, p)
	}
	if blobStore.multiHash {
		parts = append(parts, merkleId.GetMarklFormat().GetMarklFormatId())
	}
	remaining := hex
	for _, b := range blobStore.buckets {
		if len(remaining) < b {
			panic(fmt.Sprintf(
				"buckets too large for digest. buckets: %v, hex: %q",
				blobStore.buckets, hex,
			))
		}
		parts = append(parts, remaining[:b])
		remaining = remaining[b:]
	}
	parts = append(parts, remaining)
	return strings.Join(parts, "/")
}

func (blobStore *remoteS3) HasBlob(
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

	key := blobStore.keyForMarklId(merkleId)
	_, headErr := blobStore.s3Client.HeadObject(blobStore.ctx, &s3.HeadObjectInput{
		Bucket: aws.String(blobStore.config.GetBucket()),
		Key:    aws.String(key),
	})
	if headErr == nil {
		blobStore.blobCacheLock.Lock()
		blobStore.blobCache[string(merkleId.GetBytes())] = struct{}{}
		blobStore.blobCacheLock.Unlock()
		ok = true
		return ok
	}
	if isS3NotFound(headErr) {
		return false
	}
	panic(deweyerrors.Wrapf(headErr, "head s3://%s/%s",
		blobStore.config.GetBucket(), key))
}

func (blobStore *remoteS3) AllBlobs() interfaces.SeqError[domain_interfaces.MarklId] {
	blobStore.initializeOnce()

	if blobStore.multiHash {
		return blobStore.allBlobsMultiHash()
	}
	return blobStore.allBlobsForBase(
		strings.TrimRight(blobStore.config.GetPrefix(), "/"),
		blobStore.defaultHashType,
	)
}

func (blobStore *remoteS3) allBlobsMultiHash() interfaces.SeqError[domain_interfaces.MarklId] {
	return func(yield func(domain_interfaces.MarklId, error) bool) {
		bucket := blobStore.config.GetBucket()
		basePrefix := strings.TrimRight(blobStore.config.GetPrefix(), "/")

		// Walk the immediate child "directories" under prefix using a
		// delimiter; each common-prefix segment is a hash-format id.
		listPrefix := basePrefix
		if listPrefix != "" {
			listPrefix += "/"
		}

		paginator := s3.NewListObjectsV2Paginator(
			blobStore.s3Client,
			&s3.ListObjectsV2Input{
				Bucket:    aws.String(bucket),
				Prefix:    aws.String(listPrefix),
				Delimiter: aws.String("/"),
			},
		)

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(blobStore.ctx)
			if err != nil {
				if !yield(nil, deweyerrors.Wrapf(err, "list s3://%s/%s", bucket, listPrefix)) {
					return
				}
				continue
			}

			for _, cp := range page.CommonPrefixes {
				if cp.Prefix == nil {
					continue
				}
				name := strings.TrimSuffix(strings.TrimPrefix(*cp.Prefix, listPrefix), "/")
				if name == "" {
					continue
				}

				hashType, err := markl.GetFormatHashOrError(name)
				if err != nil {
					if !yield(nil, deweyerrors.Wrap(err)) {
						return
					}
					continue
				}

				subBase := name
				if basePrefix != "" {
					subBase = basePrefix + "/" + name
				}
				for id, err := range blobStore.allBlobsForBase(subBase, hashType) {
					if !yield(id, err) {
						return
					}
				}
			}
		}
	}
}

func (blobStore *remoteS3) allBlobsForBase(
	basePrefix string,
	hashType markl.FormatHash,
) interfaces.SeqError[domain_interfaces.MarklId] {
	return func(yield func(domain_interfaces.MarklId, error) bool) {
		bucket := blobStore.config.GetBucket()

		listPrefix := basePrefix
		if listPrefix != "" {
			listPrefix += "/"
		}

		paginator := s3.NewListObjectsV2Paginator(
			blobStore.s3Client,
			&s3.ListObjectsV2Input{
				Bucket: aws.String(bucket),
				Prefix: aws.String(listPrefix),
			},
		)

		digest, repool := hashType.GetBlobId()
		defer repool()

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(blobStore.ctx)
			if err != nil {
				if !yield(nil, deweyerrors.Wrapf(err, "list s3://%s/%s", bucket, listPrefix)) {
					return
				}
				continue
			}

			for _, obj := range page.Contents {
				if obj.Key == nil {
					continue
				}
				key := *obj.Key
				rel := strings.TrimPrefix(key, listPrefix)
				if shouldSkipBlobWalkEntry(stdpath.Base(rel)) {
					continue
				}

				if err := markl.SetHexStringFromRelPath(digest, rel); err != nil {
					if !yield(nil, deweyerrors.Wrap(err)) {
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
}

func (blobStore *remoteS3) MakeBlobWriter(
	marklHashType domain_interfaces.FormatHash,
) (blobWriter domain_interfaces.BlobWriter, err error) {
	blobStore.initializeOnce()

	mover := &s3Mover{
		store:  blobStore,
		config: blobStore.makeEnvDirConfig(),
	}

	hash, _ := blobStore.defaultHashType.Get() //repool:owned

	if err = mover.initialize(hash); err != nil {
		err = deweyerrors.Wrap(err)
		return blobWriter, err
	}

	blobWriter = mover

	return blobWriter, err
}

func (blobStore *remoteS3) MakeBlobReader(
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

	bucket := blobStore.config.GetBucket()
	key := blobStore.keyForMarklId(digest)

	out, getErr := blobStore.s3Client.GetObject(blobStore.ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if getErr != nil {
		if isS3NotFound(getErr) {
			clonedDigest, _ := markl.Clone(digest) //repool:owned
			err = blob_io.ErrBlobMissing{
				BlobId: clonedDigest,
				Path:   "s3://" + bucket + "/" + key,
			}
			return readCloser, err
		}
		err = deweyerrors.Wrap(getErr)
		return readCloser, err
	}

	// BlobReader requires Seek/ReadAt; S3 GetObject returns a plain
	// ReadCloser. Buffer to a tempfile so the decryption layer can
	// seek over a *os.File the way SFTP's reader does.
	tmpFile, tmpErr := os.CreateTemp("", "madder-s3-read-*")
	if tmpErr != nil {
		out.Body.Close()
		err = deweyerrors.Wrapf(tmpErr, "create temp file for s3 read")
		return readCloser, err
	}

	if _, copyErr := io.Copy(tmpFile, out.Body); copyErr != nil {
		out.Body.Close()
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		err = deweyerrors.Wrapf(copyErr, "buffer s3://%s/%s to disk", bucket, key)
		return readCloser, err
	}
	out.Body.Close()
	if _, seekErr := tmpFile.Seek(0, io.SeekStart); seekErr != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		err = deweyerrors.Wrap(seekErr)
		return readCloser, err
	}

	blobStore.blobCacheLock.Lock()
	blobStore.blobCache[string(digest.GetBytes())] = struct{}{}
	blobStore.blobCacheLock.Unlock()

	cfg := blobStore.makeEnvDirConfig()
	reader := &s3Reader{
		file:   tmpFile,
		config: cfg,
	}

	readerHash, _ := blobStore.defaultHashType.Get() //repool:owned
	if err = reader.initialize(readerHash); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		err = deweyerrors.Wrap(err)
		return readCloser, err
	}

	readCloser = reader
	return readCloser, err
}

// s3Mover buffers writes to a local tempfile (hashing/compressing as
// they flow through), then PUTs the resulting object on Close using
// the content-addressed key derived from the digest.
type s3Mover struct {
	hash         domain_interfaces.Hash
	store        *remoteS3
	config       blob_io.Config
	tempFile     *os.File
	writer       *s3Writer
	closed       bool
	bytesWritten int64
}

func (mover *s3Mover) emitWriteEvent(
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

func (mover *s3Mover) initialize(hash domain_interfaces.Hash) (err error) {
	mover.hash = hash

	var tempNameBytes [16]byte
	if _, err = rand.Read(tempNameBytes[:]); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}

	tempName := fmt.Sprintf("madder-s3-write-%x-*", tempNameBytes)
	if mover.tempFile, err = os.CreateTemp("", tempName); err != nil {
		err = deweyerrors.Wrapf(err, "unable to create temp file")
		return err
	}

	if mover.writer, err = newS3Writer(mover.config, mover.tempFile, hash); err != nil {
		closeErr := mover.tempFile.Close()
		rmErr := os.Remove(mover.tempFile.Name())
		return errors.Join(deweyerrors.Wrap(err), closeErr, rmErr)
	}

	return err
}

func (mover *s3Mover) Write(p []byte) (n int, err error) {
	if mover.writer == nil {
		err = deweyerrors.ErrorWithStackf("writer not initialized")
		return n, err
	}
	n, err = mover.writer.Write(p)
	mover.bytesWritten += int64(n)
	return n, err
}

func (mover *s3Mover) ReadFrom(r io.Reader) (n int64, err error) {
	if mover.writer == nil {
		err = deweyerrors.ErrorWithStackf("writer not initialized")
		return n, err
	}
	n, err = mover.writer.ReadFrom(r)
	mover.bytesWritten += n
	return n, err
}

func (mover *s3Mover) Close() (err error) {
	if mover.closed {
		return nil
	}
	mover.closed = true

	if mover.writer == nil {
		return nil
	}

	// mover.tempFile is set by mover.initialize concurrent with
	// mover.writer; the writer-nil check above also gates tempFile-nil.
	// LIFO defer order — os.Remove registered first, runs last — so
	// the tempfile is closed before its path is removed.
	defer func() {
		if rerr := os.Remove(mover.tempFile.Name()); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	defer deweyerrors.DeferredCloser(&err, mover.tempFile)

	if err = mover.writer.Close(); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}

	if _, err = mover.tempFile.Seek(0, io.SeekStart); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}

	blobDigest := mover.writer.GetDigest()
	key := mover.store.keyForMarklId(blobDigest)
	bucket := mover.store.config.GetBucket()

	// Content-addressed CAS: same key → same bytes, so PUT with
	// If-None-Match: * lets the server reject a concurrent overwrite
	// in a single round-trip. A HEAD-then-PUT shape would race: two
	// writers can both pass the HEAD then collide on PUT. The
	// PreconditionFailed error from a duplicate is treated as success
	// — content-equivalence is the invariant we care about.
	if _, err = mover.store.s3Client.PutObject(mover.store.ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        mover.tempFile,
		IfNoneMatch: aws.String("*"),
	}); err != nil {
		if !isS3PreconditionFailed(err) {
			err = deweyerrors.Wrapf(err, "put s3://%s/%s", bucket, key)
			return err
		}
		// Object already exists at the same key. Treat as a
		// duplicate-write success and fall through to cache update.
		err = nil
	}

	mover.emitWriteEvent(domain_interfaces.BlobWriteOpWritten, mover.bytesWritten)

	mover.store.blobCacheLock.Lock()
	mover.store.blobCache[string(blobDigest.GetBytes())] = struct{}{}
	mover.store.blobCacheLock.Unlock()

	return err
}

func (mover *s3Mover) GetMarklId() domain_interfaces.MarklId {
	if mover.writer == nil {
		return nil
	}
	return mover.writer.GetDigest()
}

// s3Writer applies compression/encryption and hashes bytes on the way
// to a local tempfile. Identical shape to sftpWriter; kept separate so
// the two stores can evolve independently.
type s3Writer struct {
	hash            domain_interfaces.Hash
	tee             io.Writer
	wCompress, wAge io.WriteCloser
	wBuf            *bufio.Writer
}

func newS3Writer(
	config blob_io.Config,
	ioWriter io.Writer,
	hash domain_interfaces.Hash,
) (writer *s3Writer, err error) {
	writer = &s3Writer{}
	writer.wBuf = bufio.NewWriter(ioWriter)

	if writer.wAge, err = config.GetBlobEncryption().WrapWriter(writer.wBuf); err != nil {
		err = deweyerrors.Wrap(err)
		return writer, err
	}

	writer.hash = hash

	if writer.wCompress, err = config.GetBlobCompression().WrapWriter(writer.wAge); err != nil {
		err = deweyerrors.Wrap(err)
		return writer, err
	}

	writer.tee = io.MultiWriter(writer.hash, writer.wCompress)
	return writer, err
}

func (writer *s3Writer) ReadFrom(r io.Reader) (n int64, err error) {
	if n, err = io.Copy(writer.tee, r); err != nil {
		err = deweyerrors.Wrap(err)
		return n, err
	}
	return n, err
}

func (writer *s3Writer) Write(p []byte) (n int, err error) {
	return writer.tee.Write(p)
}

func (writer *s3Writer) Close() (err error) {
	if writer.wCompress != nil {
		if err = writer.wCompress.Close(); err != nil {
			err = deweyerrors.Wrap(err)
			return err
		}
	}
	if writer.wAge != nil {
		if err = writer.wAge.Close(); err != nil {
			err = deweyerrors.Wrap(err)
			return err
		}
	}
	if writer.wBuf != nil {
		if err = writer.wBuf.Flush(); err != nil {
			err = deweyerrors.Wrap(err)
			return err
		}
	}
	return err
}

func (writer *s3Writer) GetDigest() domain_interfaces.MarklId {
	id, _ := writer.hash.GetMarklId() //repool:owned
	return id
}

// s3Reader is the buffer-to-tempfile counterpart of sftpReader. It
// owns the tempfile and removes it on Close.
type s3Reader struct {
	file      *os.File
	config    blob_io.Config
	hash      domain_interfaces.Hash
	decrypter io.Reader
	expander  io.ReadCloser
	tee       io.Reader
}

func (reader *s3Reader) initialize(hash domain_interfaces.Hash) (err error) {
	if reader.decrypter, err = reader.config.GetBlobEncryption().WrapReader(reader.file); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}
	if reader.expander, err = reader.config.GetBlobCompression().WrapReader(reader.decrypter); err != nil {
		err = deweyerrors.Wrap(err)
		return err
	}
	reader.hash = hash
	reader.tee = io.TeeReader(reader.expander, reader.hash)
	return err
}

func (reader *s3Reader) Read(p []byte) (n int, err error) {
	return reader.tee.Read(p)
}

func (reader *s3Reader) WriteTo(w io.Writer) (n int64, err error) {
	return io.Copy(w, reader.tee)
}

func (reader *s3Reader) Seek(offset int64, whence int) (actual int64, err error) {
	seeker, ok := reader.decrypter.(io.Seeker)
	if !ok {
		err = deweyerrors.ErrorWithStackf("seeking not supported")
		return actual, err
	}
	return seeker.Seek(offset, whence)
}

func (reader *s3Reader) ReadAt(p []byte, off int64) (n int, err error) {
	readerAt, ok := reader.decrypter.(io.ReaderAt)
	if !ok {
		err = deweyerrors.ErrorWithStackf("reading at not supported")
		return n, err
	}
	return readerAt.ReadAt(p, off)
}

func (reader *s3Reader) Close() error {
	tmpPath := reader.file.Name()
	return errors.Join(
		reader.expander.Close(),
		reader.file.Close(),
		os.Remove(tmpPath),
	)
}

func (reader *s3Reader) GetMarklId() domain_interfaces.MarklId {
	id, _ := reader.hash.GetMarklId() //repool:owned
	return id
}

// s3JoinKey joins prefix and name with a single forward slash, stripping
// any redundant trailing/leading slashes.
func s3JoinKey(prefix, name string) string {
	prefix = strings.TrimRight(prefix, "/")
	name = strings.TrimLeft(name, "/")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// isS3NotFound recognizes the various shapes the S3 / S3-compatible
// servers use for "no such key/object". HeadObject returns a smithy
// API error with code "NotFound" (no body); GetObject returns the
// typed s3types.NoSuchKey error.
func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NotFound" || code == "NoSuchKey" || code == "404" {
			return true
		}
	}
	return false
}

// ValidateS3Auth enforces credential-state invariants the AWS SDK
// would otherwise discover only on the first API call. Today's only
// rule: session-token is meaningless without access-key-id (the
// session token is a temporary credential tied to specific
// access/secret-key pairs; the SDK's credential chain would either
// ignore it or fail confusingly).
//
// Anonymous / IMDS-driven configs (none of the explicit fields set)
// are valid — the SDK's default credential chain handles those.
// Exported so init-s3's bootstrap path can validate before opening
// any HTTP connections.
func ValidateS3Auth(config blob_store_configs.ConfigS3) error {
	if config.GetSessionToken() != "" && config.GetAccessKeyId() == "" {
		return deweyerrors.Errorf(
			"s3 auth: session-token set without access-key-id",
		)
	}
	return nil
}

// isS3PreconditionFailed recognizes a PutObject's "object already
// exists" response from PreconditionFailed (HTTP 412) raised when
// the request carries `If-None-Match: *` and the target key is
// already populated. AWS S3 returns smithy APIError with code
// "PreconditionFailed"; compatible servers may use the same code or
// surface the 412 status directly.
func isS3PreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "PreconditionFailed" || code == "412" {
			return true
		}
	}
	return false
}

// errS3RemoteConfigExists signals that <prefix>/blob_store-config is
// already present in the target bucket. Callers (init-s3) treat this
// as a benign condition and reuse the existing remote config.
type errS3RemoteConfigExists struct {
	Bucket string
	Key    string
}

func (e errS3RemoteConfigExists) Error() string {
	return fmt.Sprintf(
		"remote blob_store-config already present at s3://%s/%s",
		e.Bucket, e.Key,
	)
}

// IsRemoteConfigAlreadyExists reports whether err signals that the
// remote blob_store-config object already exists in the target bucket.
func IsRemoteConfigAlreadyExists(err error) bool {
	var e errS3RemoteConfigExists
	return errors.As(err, &e)
}

// WriteRemoteConfigS3 PUTs a default blob_store-config object at
// <prefix>/blob_store-config in the bucket, mirroring the SFTP-side
// WriteRemoteConfig. Returns errS3RemoteConfigExists when the object
// already exists (callers can decide whether that's an error).
// Used by the init-s3 command's bootstrap path.
func WriteRemoteConfigS3(
	ctx context.Context,
	client *s3.Client,
	cfg blob_store_configs.ConfigS3,
	discovered DiscoveredConfig,
	uiPrinter ui.Printer,
) error {
	bucket := cfg.GetBucket()
	key := s3JoinKey(cfg.GetPrefix(), directory_layout.FileNameBlobStoreConfig)

	uiPrinter.Printf("writing remote blob store config to s3://%s/%s ...", bucket, key)

	_, headErr := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if headErr == nil {
		return errS3RemoteConfigExists{Bucket: bucket, Key: key}
	}
	if !isS3NotFound(headErr) {
		return deweyerrors.Wrapf(headErr, "head s3://%s/%s", bucket, key)
	}

	config := configFromDiscoveredConfig(discovered)
	typedConfig := &blob_store_configs.TypedConfig{
		Type: typeStructForCurrentLocalConfig(),
		Blob: config,
	}

	var buf bytes.Buffer
	if _, err := blob_store_configs.Coder.EncodeTo(typedConfig, &buf); err != nil {
		return deweyerrors.Wrapf(err, "encode remote blob store config")
	}

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(buf.Bytes()),
	}); err != nil {
		return deweyerrors.Wrapf(err, "put s3://%s/%s", bucket, key)
	}

	return nil
}

func typeStructForCurrentLocalConfig() ids.TypeStruct {
	return ids.GetOrPanic(ids.TypeTomlBlobStoreConfigVCurrent).TypeStruct
}
