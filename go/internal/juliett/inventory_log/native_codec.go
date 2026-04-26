package inventory_log

import (
	"encoding/json"
	"os"

	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/0/ids"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
)

// blobWriteRecord is the on-disk JSON shape for blob-write-published-v1
// events. Field tags match ADR 0004's record schema with the addition of a
// top-level "type" field for codec dispatch and a switch from RFC3339Nano
// to TAI sec.asec for the timestamp.
type blobWriteRecord struct {
	Type        string `json:"type"`
	Ts          string `json:"ts"`
	Utility     string `json:"utility"`
	Pid         int    `json:"pid"`
	StoreId     string `json:"store_id"`
	MarklId     string `json:"markl_id"`
	Size        int64  `json:"size"`
	Op          string `json:"op"`
	Description string `json:"description,omitempty"`
}

// nowTai and pidNow are package-level injection points for deterministic
// tests of the native codec. Production callers should not touch them.
var (
	nowTai = ids.NowTai
	pidNow = os.Getpid
)

func init() {
	registerNative(MakeCodec[domain_interfaces.BlobWriteEvent](
		"blob-write-published-v1",
		encodeBlobWritePublished,
		decodeBlobWritePublished,
	))
}

func encodeBlobWritePublished(ev domain_interfaces.BlobWriteEvent) ([]byte, error) {
	marklStr := ""
	if ev.MarklId != nil {
		marklStr = markl.FormatBytesAsHex(ev.MarklId)
	}

	return json.Marshal(blobWriteRecord{
		Type:        "blob-write-published-v1",
		Ts:          nowTai().String(),
		Utility:     "madder",
		Pid:         pidNow(),
		StoreId:     ev.StoreId,
		MarklId:     marklStr,
		Size:        ev.Size,
		Op:          string(ev.Op),
		Description: ev.Description,
	})
}

func decodeBlobWritePublished(line []byte) (domain_interfaces.BlobWriteEvent, error) {
	var rec blobWriteRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return domain_interfaces.BlobWriteEvent{}, err
	}

	// Ts, Pid, Utility are observer-level metadata, not BlobWriteEvent
	// fields, so they are dropped here. MarklId is also dropped: there is
	// no single canonical hex-to-MarklId path in this layer, and tools
	// that need round-trip parsing should consume the JSON directly.
	return domain_interfaces.BlobWriteEvent{
		StoreId:     rec.StoreId,
		Size:        rec.Size,
		Op:          domain_interfaces.BlobWriteOp(rec.Op),
		Description: rec.Description,
	}, nil
}
