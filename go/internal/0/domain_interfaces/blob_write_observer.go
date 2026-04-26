package domain_interfaces

// BlobWriteOp is the disposition of a single blob publish attempt, as
// determined in the env_dir mover's link(2) branch. Four values are defined
// to cover the ADR 0002 / 0003 / issue #31 interaction:
//
//   - BlobWriteOpWritten: link(2) returned nil, a new inode was published.
//   - BlobWriteOpExists: link(2) returned EEXIST, verify-on-collision was off.
//   - BlobWriteOpVerifyMatch: EEXIST + verify enabled + bytes matched.
//   - BlobWriteOpVerifyMismatch: EEXIST + verify enabled + bytes differed
//     (reported on the way out; the mismatch error is still returned).
type BlobWriteOp string

const (
	BlobWriteOpWritten        BlobWriteOp = "written"
	BlobWriteOpExists         BlobWriteOp = "exists"
	BlobWriteOpVerifyMatch    BlobWriteOp = "verify-match"
	BlobWriteOpVerifyMismatch BlobWriteOp = "verify-mismatch"
)

// LogEvent is the discriminator interface for every event written to the
// inventory log. The returned string identifies the event type both in
// the on-disk JSON payload (the top-level `"type"` field) and in the
// inventory_log codec registry. Stable, public — once shipped, a
// `! type-string` cannot change without breaking on-disk readers.
type LogEvent interface {
	LogType() string
}

// BlobWriteEvent is emitted once per blob publish. StoreId is the
// blob-store-id.Id stringified at the call site (interface lives at layer
// 0 and cannot import alfa/blob_store_id). Size is the byte length of the
// temp file at the moment of link(2), stat'd before file.Close().
type BlobWriteEvent struct {
	StoreId     string
	MarklId     MarklId
	Size        int64
	Op          BlobWriteOp
	Description string
}

// LogType identifies BlobWriteEvent in the inventory log.
//
// Type-string is reserved by inventory_log; see the reserved-types map
// in that package. Importer codecs cannot register against this type.
func (BlobWriteEvent) LogType() string { return "blob-write-published-v1" }

var _ LogEvent = BlobWriteEvent{}

// BlobWriteObserver is called from concrete blob-store publish paths once
// per attempt. Implementations must not fail the blob write — errors are
// captured out-of-band (debug.Options, etc.) per xdg_log_home(7).
type BlobWriteObserver interface {
	OnBlobPublished(event BlobWriteEvent)
}
