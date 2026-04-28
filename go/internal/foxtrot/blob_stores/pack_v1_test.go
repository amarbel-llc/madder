//go:build test

package blob_stores

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	goerrors "errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/alfa/inventory_archive"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
	"github.com/amarbel-llc/madder/go/internal/delta/blob_store_configs"
	"github.com/amarbel-llc/purse-first/libs/dewey/bravo/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/delta/compression_type"
)

func TestPackV1WithDelta(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	// Create similar blobs that should benefit from delta encoding.
	// The blobs share a large common prefix to produce small deltas.
	commonPrefix := strings.Repeat("shared content block ", 100)
	testData1 := []byte(commonPrefix + " unique suffix alpha")
	testData2 := []byte(commonPrefix + " unique suffix beta")
	testData3 := []byte(commonPrefix + " unique suffix gamma")

	rawHash1 := sha256.Sum256(testData1)
	rawHash2 := sha256.Sum256(testData2)
	rawHash3 := sha256.Sum256(testData3)

	id1, repool1 := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(rawHash1[:]),
	)
	defer repool1()

	id2, repool2 := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(rawHash2[:]),
	)
	defer repool2()

	id3, repool3 := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(rawHash3[:]),
	)
	defer repool3()

	stub := &stubBlobStore{
		allBlobIds: []domain_interfaces.MarklId{id1, id2, id3},
		blobData: map[string][]byte{
			id1.String(): testData1,
			id2.String(): testData2,
			id3.String(): testData3,
		},
	}

	config := blob_store_configs.TomlInventoryArchiveV1{
		HashTypeId:      markl.FormatIdHashSha256,
		CompressionType: compression_type.CompressionTypeNone,
		Delta: blob_store_configs.DeltaConfig{
			Enabled:     true,
			Algorithm:   "bsdiff",
			MinBlobSize: 1,
			MaxBlobSize: 10485760,
			SizeRatio:   2.0,
		},
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config:         config,
	}

	if err := store.Pack(PackOptions{}); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Verify all blobs are in the in-memory index.
	if !store.HasBlob(id1) {
		t.Fatal("expected id1 in index after pack")
	}

	if !store.HasBlob(id2) {
		t.Fatal("expected id2 in index after pack")
	}

	if !store.HasBlob(id3) {
		t.Fatal("expected id3 in index after pack")
	}

	// Verify at least one entry was stored as delta.
	deltaCount := 0
	for _, entry := range store.index {
		if entry.EntryType == inventory_archive.EntryTypeDelta {
			deltaCount++
		}
	}

	if deltaCount == 0 {
		t.Fatal("expected at least one delta entry, got none")
	}

	t.Logf("delta entries: %d, total entries: %d", deltaCount, len(store.index))

	// Verify all blobs are readable and produce the correct data.
	for _, tc := range []struct {
		name string
		id   domain_interfaces.MarklId
		data []byte
	}{
		{"blob1", id1, testData1},
		{"blob2", id2, testData2},
		{"blob3", id3, testData3},
	} {
		reader, err := store.MakeBlobReader(tc.id)
		if err != nil {
			t.Fatalf("MakeBlobReader for %s: %v", tc.name, err)
		}

		got, err := io.ReadAll(reader)
		reader.Close()

		if err != nil {
			t.Fatalf("ReadAll for %s: %v", tc.name, err)
		}

		if !bytes.Equal(got, tc.data) {
			t.Errorf(
				"%s data mismatch: got %d bytes, want %d bytes",
				tc.name,
				len(got),
				len(tc.data),
			)
		}
	}

	// Verify v1 data file was written.
	archivesPath := filepath.Join(basePath, "archives")

	dataMatches, err := filepath.Glob(
		filepath.Join(archivesPath, "*"+inventory_archive.DataFileExtensionV1),
	)
	if err != nil {
		t.Fatalf("globbing data files: %v", err)
	}

	if len(dataMatches) != 1 {
		t.Fatalf("expected 1 v1 data file, got %d", len(dataMatches))
	}

	// Verify v1 index file was written.
	indexMatches, err := filepath.Glob(
		filepath.Join(archivesPath, "*"+inventory_archive.IndexFileExtensionV1),
	)
	if err != nil {
		t.Fatalf("globbing index files: %v", err)
	}

	if len(indexMatches) != 1 {
		t.Fatalf("expected 1 v1 index file, got %d", len(indexMatches))
	}

	// Verify v1 cache file was written.
	cacheFilePath := filepath.Join(
		cachePath,
		inventory_archive.CacheFileNameV1,
	)

	if _, err := os.Stat(cacheFilePath); err != nil {
		t.Fatalf("expected v1 cache file at %s: %v", cacheFilePath, err)
	}
}

func TestPackV1WithoutDelta(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	testData1 := []byte("pack v1 no-delta blob one")
	testData2 := []byte("pack v1 no-delta blob two")

	rawHash1 := sha256.Sum256(testData1)
	rawHash2 := sha256.Sum256(testData2)

	id1, repool1 := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(rawHash1[:]),
	)
	defer repool1()

	id2, repool2 := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(rawHash2[:]),
	)
	defer repool2()

	stub := &stubBlobStore{
		allBlobIds: []domain_interfaces.MarklId{id1, id2},
		blobData: map[string][]byte{
			id1.String(): testData1,
			id2.String(): testData2,
		},
	}

	config := blob_store_configs.TomlInventoryArchiveV1{
		HashTypeId:      markl.FormatIdHashSha256,
		CompressionType: compression_type.CompressionTypeNone,
		Delta: blob_store_configs.DeltaConfig{
			Enabled:     false,
			Algorithm:   "bsdiff",
			MinBlobSize: 1,
			MaxBlobSize: 10485760,
			SizeRatio:   2.0,
		},
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config:         config,
	}

	if err := store.Pack(PackOptions{}); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Verify all blobs are in the index.
	if !store.HasBlob(id1) {
		t.Fatal("expected id1 in index after pack")
	}

	if !store.HasBlob(id2) {
		t.Fatal("expected id2 in index after pack")
	}

	// Verify all entries are full (no deltas).
	for key, entry := range store.index {
		if entry.EntryType != inventory_archive.EntryTypeFull {
			t.Errorf(
				"expected full entry for %s, got entry type %d",
				key,
				entry.EntryType,
			)
		}
	}

	// Verify all blobs are readable.
	for _, tc := range []struct {
		name string
		id   domain_interfaces.MarklId
		data []byte
	}{
		{"blob1", id1, testData1},
		{"blob2", id2, testData2},
	} {
		reader, err := store.MakeBlobReader(tc.id)
		if err != nil {
			t.Fatalf("MakeBlobReader for %s: %v", tc.name, err)
		}

		got, err := io.ReadAll(reader)
		reader.Close()

		if err != nil {
			t.Fatalf("ReadAll for %s: %v", tc.name, err)
		}

		if !bytes.Equal(got, tc.data) {
			t.Errorf("%s data mismatch", tc.name)
		}
	}
}

func TestPackV1DeltaFallsBackToFullWhenLarger(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	// Create blobs with truly random content so deltas are guaranteed to be
	// larger than the original data, triggering the trial-and-discard fallback.
	var blobDatas [][]byte
	var blobIds []domain_interfaces.MarklId

	blobData := make(map[string][]byte)

	for i := range 3 {
		// Random data is incompressible and produces deltas larger than
		// the original. Use similar sizes so the selector still pairs them.
		data := make([]byte, 2048+i*100)
		if _, randErr := rand.Read(data); randErr != nil {
			t.Fatalf("crypto/rand.Read: %v", randErr)
		}

		blobDatas = append(blobDatas, data)

		rawHash := sha256.Sum256(data)
		id, repool := hashFormat.GetBlobIdForHexString(
			hex.EncodeToString(rawHash[:]),
		)
		defer repool()

		blobIds = append(blobIds, id)
		blobData[id.String()] = data
	}

	stub := &stubBlobStore{
		allBlobIds: blobIds,
		blobData:   blobData,
	}

	config := blob_store_configs.TomlInventoryArchiveV1{
		HashTypeId:      markl.FormatIdHashSha256,
		CompressionType: compression_type.CompressionTypeNone,
		Delta: blob_store_configs.DeltaConfig{
			Enabled:     true,
			Algorithm:   "bsdiff",
			MinBlobSize: 1,
			MaxBlobSize: 10485760,
			SizeRatio:   2.0,
		},
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config:         config,
	}

	if err := store.Pack(PackOptions{}); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Verify all blobs are in the index.
	for i, id := range blobIds {
		if !store.HasBlob(id) {
			t.Fatalf("expected blob %d in index after pack", i)
		}
	}

	// Verify all blobs are readable and produce correct data.
	for i, id := range blobIds {
		reader, err := store.MakeBlobReader(id)
		if err != nil {
			t.Fatalf("MakeBlobReader for blob %d: %v", i, err)
		}

		got, err := io.ReadAll(reader)
		reader.Close()

		if err != nil {
			t.Fatalf("ReadAll for blob %d: %v", i, err)
		}

		if !bytes.Equal(got, blobDatas[i]) {
			t.Errorf(
				"blob %d data mismatch: got %d bytes, want %d bytes",
				i,
				len(got),
				len(blobDatas[i]),
			)
		}
	}

	// All entries must be stored as full since the random content produces
	// deltas larger than the original data.
	fullCount := 0
	for _, entry := range store.index {
		if entry.EntryType == inventory_archive.EntryTypeFull {
			fullCount++
		}
	}

	if fullCount != len(store.index) {
		t.Errorf(
			"expected all entries to be full, got %d full out of %d",
			fullCount,
			len(store.index),
		)
	}
}

func TestPackV1SplitsWhenExceedingMaxPackSize(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	// Create 4 blobs of ~100 bytes each. Set MaxPackSize to 250 so we
	// get 2 pack files (2 blobs each).
	var blobIds []domain_interfaces.MarklId
	blobData := make(map[string][]byte)
	var allData [][]byte

	for i := range 4 {
		data := bytes.Repeat([]byte{byte('a' + i)}, 100)
		allData = append(allData, data)

		rawHash := sha256.Sum256(data)
		id, repool := hashFormat.GetBlobIdForHexString(
			hex.EncodeToString(rawHash[:]),
		)
		defer repool()

		blobIds = append(blobIds, id)
		blobData[id.String()] = data
	}

	stub := &stubBlobStore{
		allBlobIds: blobIds,
		blobData:   blobData,
	}

	config := blob_store_configs.TomlInventoryArchiveV1{
		HashTypeId:      markl.FormatIdHashSha256,
		CompressionType: compression_type.CompressionTypeNone,
		MaxPackSize:     250,
		Delta: blob_store_configs.DeltaConfig{
			Enabled:     false,
			Algorithm:   "bsdiff",
			MinBlobSize: 1,
			MaxBlobSize: 10485760,
			SizeRatio:   2.0,
		},
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config:         config,
	}

	if err := store.Pack(PackOptions{}); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Verify all blobs are in the index.
	for i, id := range blobIds {
		if !store.HasBlob(id) {
			t.Fatalf("expected blob %d in index after pack", i)
		}
	}

	// Verify all blobs are readable with correct data.
	for i, id := range blobIds {
		reader, err := store.MakeBlobReader(id)
		if err != nil {
			t.Fatalf("MakeBlobReader for blob %d: %v", i, err)
		}

		got, err := io.ReadAll(reader)
		reader.Close()

		if err != nil {
			t.Fatalf("ReadAll for blob %d: %v", i, err)
		}

		if !bytes.Equal(got, allData[i]) {
			t.Errorf("blob %d data mismatch", i)
		}
	}

	// Verify multiple data files were created (split happened).
	archivesPath := filepath.Join(basePath, "archives")

	dataMatches, err := filepath.Glob(
		filepath.Join(archivesPath, "*"+inventory_archive.DataFileExtensionV1),
	)
	if err != nil {
		t.Fatalf("globbing data files: %v", err)
	}

	if len(dataMatches) < 2 {
		t.Fatalf("expected at least 2 data files (split), got %d", len(dataMatches))
	}
}

// TestPackV1_BlobFilterRestrictsToSubset confirms that PackOptions.BlobFilter
// limits packing to the named ids. Three loose blobs are present, the filter
// names two of them, and only those two land in the archive.
func TestPackV1_BlobFilterRestrictsToSubset(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	mkBlob := func(suffix byte) (domain_interfaces.MarklId, func(), []byte) {
		data := bytes.Repeat([]byte{suffix}, 64)
		raw := sha256.Sum256(data)
		id, repool := hashFormat.GetBlobIdForHexString(
			hex.EncodeToString(raw[:]),
		)
		return id, repool, data
	}

	id1, repool1, data1 := mkBlob('a')
	defer repool1()
	id2, repool2, data2 := mkBlob('b')
	defer repool2()
	id3, repool3, data3 := mkBlob('c')
	defer repool3()

	stub := &stubBlobStore{
		allBlobIds: []domain_interfaces.MarklId{id1, id2, id3},
		blobData: map[string][]byte{
			id1.String(): data1,
			id2.String(): data2,
			id3.String(): data3,
		},
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config: blob_store_configs.TomlInventoryArchiveV1{
			HashTypeId:      markl.FormatIdHashSha256,
			CompressionType: compression_type.CompressionTypeNone,
			Delta: blob_store_configs.DeltaConfig{
				Enabled:     false,
				Algorithm:   "bsdiff",
				MinBlobSize: 1,
				MaxBlobSize: 10485760,
				SizeRatio:   2.0,
			},
		},
	}

	if err := store.Pack(PackOptions{
		BlobFilter: map[string]domain_interfaces.MarklId{
			id1.String(): id1,
			id3.String(): id3,
		},
	}); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	// Filter included id1 and id3; id2 must NOT be in the archive.
	if !store.HasBlob(id1) {
		t.Errorf("expected id1 in archive (filter included it)")
	}
	if !store.HasBlob(id3) {
		t.Errorf("expected id3 in archive (filter included it)")
	}

	// HasBlob falls through to the loose store, so the assertion has
	// to be against the index directly — id2 lives in loose only.
	if _, inArchive := store.index[id2.String()]; inArchive {
		t.Errorf("expected id2 NOT in archive (filter excluded it)")
	}
}

// TestPackV1_SkipMissingBlobsContinuesOnUnreadable confirms that a loose
// blob whose MakeBlobReader errors out is skipped (not aborted) when
// SkipMissingBlobs is true. The good blobs in the same pack still land
// in the archive.
func TestPackV1_SkipMissingBlobsContinuesOnUnreadable(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	goodData := []byte("good blob content for skip-missing test")
	goodHash := sha256.Sum256(goodData)
	goodId, goodRepool := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(goodHash[:]),
	)
	defer goodRepool()

	// The "missing" blob's id is announced by AllBlobs but its data is
	// absent from the stub's blobData map. The unreadableStub overrides
	// MakeBlobReader to error on that specific id.
	missingData := []byte("missing blob would-be content")
	missingHash := sha256.Sum256(missingData)
	missingId, missingRepool := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(missingHash[:]),
	)
	defer missingRepool()

	stub := &unreadableStub{
		stubBlobStore: stubBlobStore{
			allBlobIds: []domain_interfaces.MarklId{goodId, missingId},
			blobData: map[string][]byte{
				goodId.String(): goodData,
			},
		},
		unreadableId: missingId.String(),
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config: blob_store_configs.TomlInventoryArchiveV1{
			HashTypeId:      markl.FormatIdHashSha256,
			CompressionType: compression_type.CompressionTypeNone,
			Delta: blob_store_configs.DeltaConfig{
				Enabled:     false,
				Algorithm:   "bsdiff",
				MinBlobSize: 1,
				MaxBlobSize: 10485760,
				SizeRatio:   2.0,
			},
		},
	}

	if err := store.Pack(PackOptions{SkipMissingBlobs: true}); err != nil {
		t.Fatalf("Pack with SkipMissingBlobs=true: %v", err)
	}

	if !store.HasBlob(goodId) {
		t.Errorf("expected goodId in archive after pack")
	}
	// The missing blob was announced by AllBlobs so it became a
	// candidate; SkipMissingBlobs lets pack continue past the read
	// error rather than ending up in the index.
	if _, inArchive := store.index[missingId.String()]; inArchive {
		t.Errorf("expected missingId NOT in archive (was unreadable)")
	}
}

// TestPackV1_ContextCancellationAborts confirms that a cancelled
// PackOptions.Context aborts pack early. The stub stores enough blobs
// that the pack would otherwise succeed; pre-cancelling the context
// makes Pack return ctx.Err immediately.
func TestPackV1_ContextCancellationAborts(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	var allIds []domain_interfaces.MarklId
	blobData := make(map[string][]byte)
	for i := range 4 {
		data := bytes.Repeat([]byte{byte('p' + i)}, 64)
		raw := sha256.Sum256(data)
		id, repool := hashFormat.GetBlobIdForHexString(
			hex.EncodeToString(raw[:]),
		)
		defer repool()
		allIds = append(allIds, id)
		blobData[id.String()] = data
	}

	stub := &stubBlobStore{
		allBlobIds: allIds,
		blobData:   blobData,
	}

	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: stub,
		index:          make(map[string]archiveEntryV1),
		config: blob_store_configs.TomlInventoryArchiveV1{
			HashTypeId:      markl.FormatIdHashSha256,
			CompressionType: compression_type.CompressionTypeNone,
			Delta: blob_store_configs.DeltaConfig{
				Enabled:     false,
				Algorithm:   "bsdiff",
				MinBlobSize: 1,
				MaxBlobSize: 10485760,
				SizeRatio:   2.0,
			},
		},
	}

	stdCtx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel: pack must observe Done() at first checkpoint

	activeCtx := errors.MakeContext(stdCtx)

	err := store.Pack(PackOptions{Context: activeCtx})
	if err == nil {
		t.Fatal("expected error from cancelled-context Pack, got nil")
	}
	if !goerrors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	// Index must be empty — pack aborted before publishing any archive.
	if len(store.index) > 0 {
		t.Errorf(
			"expected empty index after cancelled pack, got %d entries",
			len(store.index),
		)
	}

	// No archive files on disk either.
	matches, _ := filepath.Glob(
		filepath.Join(basePath, "archives", "*"+inventory_archive.DataFileExtensionV1),
	)
	if len(matches) > 0 {
		t.Errorf("expected no archive files after cancelled pack, got %d", len(matches))
	}
}

// TestMakeBlobReaderV1_ChainedDeltaIsError confirms that v1 read refuses
// a delta entry whose base is itself a delta. The error path lives at
// store_inventory_archive_v1.go:402-407 and is triggered when the chain
// resolves through MakeBlobReader: read the requested entry, look up its
// base by hash in the index, read the base entry — if that's also a
// delta, fail rather than silently reconstruct from a delta-of-delta.
func TestMakeBlobReaderV1_ChainedDeltaIsError(t *testing.T) {
	basePath := t.TempDir()
	cachePath := t.TempDir()

	hashFormat := markl.FormatHashSha256

	dataA := bytes.Repeat([]byte{'A'}, 64)
	hashA := sha256.Sum256(dataA)
	hashB := sha256.Sum256(append([]byte{}, append(dataA, 'B')...))
	hashC := sha256.Sum256(append([]byte{}, append(dataA, 'C')...))

	idC, repoolC := hashFormat.GetBlobIdForHexString(
		hex.EncodeToString(hashC[:]),
	)
	defer repoolC()

	// Build the archive on disk with: full-A, delta-B(base=A),
	// delta-C(base=B). C is what we'll request; the read attempts to
	// follow the chain through B, sees B is itself a delta, errors.
	if err := os.MkdirAll(filepath.Join(basePath, "archives"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	var archiveBuf bytes.Buffer
	writer, err := inventory_archive.NewDataWriterV1(
		&archiveBuf,
		markl.FormatIdHashSha256,
		compression_type.CompressionTypeNone,
		inventory_archive.FlagHasDeltas,
		nil,
	)
	if err != nil {
		t.Fatalf("NewDataWriterV1: %v", err)
	}

	if err := writer.WriteFullEntry(hashA[:], dataA); err != nil {
		t.Fatalf("WriteFullEntry(A): %v", err)
	}
	// B and C carry garbage delta payloads — the chained-delta check
	// fires before any reconstruction is attempted, so the payloads
	// are never decoded.
	if err := writer.WriteDeltaEntry(
		hashB[:],
		inventory_archive.DeltaAlgorithmByteBsdiff,
		hashA[:],
		uint64(len(dataA)+1),
		[]byte("delta-B-payload"),
	); err != nil {
		t.Fatalf("WriteDeltaEntry(B): %v", err)
	}
	if err := writer.WriteDeltaEntry(
		hashC[:],
		inventory_archive.DeltaAlgorithmByteBsdiff,
		hashB[:], // <-- chain: C's base is B, which is itself a delta
		uint64(len(dataA)+1),
		[]byte("delta-C-payload"),
	); err != nil {
		t.Fatalf("WriteDeltaEntry(C): %v", err)
	}

	checksum, entries, err := writer.Close()
	if err != nil {
		t.Fatalf("writer.Close: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries written, got %d", len(entries))
	}

	archiveChecksumHex := hex.EncodeToString(checksum)
	archivePath := filepath.Join(
		basePath, "archives",
		archiveChecksumHex+inventory_archive.DataFileExtensionV1,
	)
	if err := os.WriteFile(archivePath, archiveBuf.Bytes(), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Build the in-memory index by hand. The store would normally
	// load this from a .index file; for this targeted test we skip
	// that step and stamp the entries directly so the read path has
	// what it needs to traverse the chain.
	store := inventoryArchiveV1{
		defaultHash:    hashFormat,
		basePath:       basePath,
		cachePath:      cachePath,
		looseBlobStore: &stubBlobStore{},
		index:          make(map[string]archiveEntryV1),
	}

	for _, e := range entries {
		idForHash, repool := hashFormat.GetBlobIdForHexString(
			hex.EncodeToString(e.Hash),
		)
		store.index[idForHash.String()] = archiveEntryV1{
			ArchiveChecksum: archiveChecksumHex,
			Offset:          e.Offset,
			StoredSize:      e.StoredSize,
			EntryType:       e.EntryType,
			BaseOffset:      0, // unused on the read path; lookup is by base hash
		}
		repool()
	}

	reader, err := store.MakeBlobReader(idC)
	if err == nil {
		_ = reader.Close()
		t.Fatal("expected chained-delta error, got nil")
	}

	if !strings.Contains(err.Error(), "chained deltas not supported") {
		t.Errorf("expected chained-delta error, got: %v", err)
	}
}

// unreadableStub overrides stubBlobStore.MakeBlobReader to return an
// error when called for a specific id. Used by
// TestPackV1_SkipMissingBlobsContinuesOnUnreadable.
type unreadableStub struct {
	stubBlobStore
	unreadableId string
}

func (s *unreadableStub) MakeBlobReader(
	id domain_interfaces.MarklId,
) (domain_interfaces.BlobReader, error) {
	if id.String() == s.unreadableId {
		return nil, goerrors.New("simulated unreadable blob")
	}
	return s.stubBlobStore.MakeBlobReader(id)
}
