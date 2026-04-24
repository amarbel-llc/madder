package write_log

import (
	"github.com/amarbel-llc/madder/go/internal/0/domain_interfaces"
	"github.com/amarbel-llc/madder/go/internal/bravo/markl"
)

// Record is the NDJSON shape written to blob-writes-YYYY-MM-DD.ndjson.
// Field tags match ADR 0004's example record verbatim so the on-disk
// schema is contract-stable; adding fields is safe, renaming is not.
type Record struct {
	Ts          string `json:"ts"`
	Utility     string `json:"utility"`
	Pid         int    `json:"pid"`
	StoreId     string `json:"store_id"`
	MarklId     string `json:"markl_id"`
	Size        int64  `json:"size"`
	Op          string `json:"op"`
	Description string `json:"description,omitempty"`
}

// recordFromEvent converts a domain BlobWriteEvent plus the ambient ts and
// pid into the serialization-ready shape. Pulled out for test coverage.
func recordFromEvent(ev domain_interfaces.BlobWriteEvent, ts string, pid int) Record {
	marklStr := ""
	if ev.MarklId != nil {
		marklStr = markl.FormatBytesAsHex(ev.MarklId)
	}

	return Record{
		Ts:          ts,
		Utility:     "madder",
		Pid:         pid,
		StoreId:     ev.StoreId,
		MarklId:     marklStr,
		Size:        ev.Size,
		Op:          string(ev.Op),
		Description: ev.Description,
	}
}
