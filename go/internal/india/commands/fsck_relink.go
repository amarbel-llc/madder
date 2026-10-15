package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"code.linenisgreat.com/madder/go/internal/charlie/output_format"
	"code.linenisgreat.com/madder/go/internal/foxtrot/blob_stores"
	"code.linenisgreat.com/madder/go/internal/golf/command_components"
	"code.linenisgreat.com/piggy/go/pkgs/markl"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/ui"
)

// relinkRecord is one NDJSON line of `fsck -relink-report`.
type relinkRecord struct {
	Type             string `json:"type"`
	Store            string `json:"store"`
	Id               string `json:"id"`
	Native           string `json:"native,omitempty"`
	NativeExists     bool   `json:"native_exists"`
	PlaintextMatches bool   `json:"plaintext_matches"`
	Bytes            int64  `json:"bytes"`
	StoredBytes      int64  `json:"stored_bytes"`
	ReclaimableBytes int64  `json:"reclaimable_bytes"`
	State            string `json:"state"`
	Error            string `json:"error,omitempty"`
}

// relinkSummary is the final NDJSON line per store.
type relinkSummary struct {
	Type             string `json:"type"`
	Store            string `json:"store"`
	Checked          int    `json:"checked"`
	Relinkable       int    `json:"relinkable"`
	PlaintextDiffers int    `json:"plaintext_mismatch"`
	NativeMissing    int    `json:"native_missing"`
	Unreadable       int    `json:"unreadable"`
	ReclaimableBytes int64  `json:"reclaimable_bytes"`
}

// runRelinkReport is the read-only `fsck -relink-report` mode. For every
// full copy of a blob stored under a sha256 id (not an alias) it reads the
// bytes once, hashes them as sha256 and as blake2b256, and says whether the
// blake2b256 native object exists, so a later relink knows what it may
// replace by an alias and how many bytes that frees. It writes nothing to
// any store.
//
// -limit counts candidate sha256 copies examined, not every blob listed.
func (cmd Fsck) runRelinkReport(
	envBlobStore command_components.BlobStoreEnv,
	blobStores blob_stores.BlobStoreMap,
) (mismatches int, err error) {
	asJSON := false

	switch cmd.Format.Resolve(os.Stdout) {
	case output_format.FormatJSON, output_format.FormatNDJSON:
		asJSON = true
	}

	var out io.Writer = os.Stdout
	encoder := json.NewEncoder(out)

	for storeId, blobStore := range blobStores {
		store := blobStore.GetBlobStore()

		reporter, ok := store.(blob_stores.ForeignDigestAliasReporter)
		if !ok {
			return mismatches, errors.Errorf(
				"fsck -relink-report: blob store %q cannot tell aliases from full copies; "+
					"only sftp stores support it",
				storeId,
			)
		}

		ui.Err().Printf("(blob_store: %s) relink report: listing...", storeId)

		var (
			summary = relinkSummary{Type: "summary", Store: storeId}
			// The listing has to finish before any object is checked:
			// IsForeignDigestAlias answers from what the listing saw.
			candidates []blob_stores.RelinkRecord
		)

		for id, listErr := range store.AllBlobs() {
			if listErr != nil {
				return mismatches, errors.Wrapf(
					listErr, "fsck -relink-report: listing %q", storeId,
				)
			}

			if id.GetMarklFormat().GetMarklFormatId() != markl.FormatIdHashSha256 ||
				reporter.IsForeignDigestAlias(id) {
				continue
			}

			candidates = append(candidates, blob_stores.RelinkRecord{Id: id})

			if cmd.Limit > 0 && len(candidates) >= cmd.Limit {
				break
			}
		}

		ui.Err().Printf(
			"(blob_store: %s) relink report: checking %d sha256 copies...",
			storeId,
			len(candidates),
		)

		for i, candidate := range candidates {
			errors.ContextContinueOrPanic(envBlobStore)

			record := blob_stores.CheckRelinkCandidate(
				store,
				candidate.Id,
				markl.FormatHashBlake2b256,
			)

			summary.Checked++
			summary.ReclaimableBytes += record.ReclaimableBytes

			switch record.State {
			case blob_stores.RelinkRelinkable:
				summary.Relinkable++
			case blob_stores.RelinkPlaintextMismatch:
				summary.PlaintextDiffers++
			case blob_stores.RelinkNativeMissing:
				summary.NativeMissing++
			case blob_stores.RelinkUnreadable:
				summary.Unreadable++
			}

			if record.Mismatch() {
				mismatches++
			}

			if asJSON {
				if err = encoder.Encode(makeRelinkRecord(storeId, record)); err != nil {
					return mismatches, errors.Wrap(err)
				}
			} else {
				fmt.Fprintln(out, formatRelinkLine(record))
			}

			if (i+1)%1000 == 0 {
				ui.Err().Printf(
					"(blob_store: %s) relink report: %d of %d checked",
					storeId, i+1, len(candidates),
				)
			}
		}

		if asJSON {
			if err = encoder.Encode(summary); err != nil {
				return mismatches, errors.Wrap(err)
			}
		}

		ui.Err().Printf(
			"(blob_store: %s) relink report: %d checked, %d relinkable (%d bytes reclaimable), "+
				"%d plaintext mismatch, %d native missing, %d unreadable",
			storeId,
			summary.Checked,
			summary.Relinkable,
			summary.ReclaimableBytes,
			summary.PlaintextDiffers,
			summary.NativeMissing,
			summary.Unreadable,
		)
	}

	return mismatches, nil
}

func makeRelinkRecord(store string, record blob_stores.RelinkRecord) relinkRecord {
	out := relinkRecord{
		Type:             "object",
		Store:            store,
		Id:               record.Id.String(),
		NativeExists:     record.NativeExists,
		PlaintextMatches: record.PlaintextMatches,
		Bytes:            record.Bytes,
		StoredBytes:      record.StoredBytes,
		ReclaimableBytes: record.ReclaimableBytes,
		State:            string(record.State),
	}

	if record.Native != nil {
		out.Native = record.Native.String()
	}

	if record.Err != nil {
		out.Error = record.Err.Error()
	}

	return out
}

func formatRelinkLine(record blob_stores.RelinkRecord) string {
	var line strings.Builder

	fmt.Fprintf(&line, "%s %s", record.State, record.Id)

	if record.Native != nil {
		fmt.Fprintf(&line, " -> %s", record.Native)
	}

	fmt.Fprintf(&line, " (%d bytes)", record.Bytes)

	if record.Err != nil {
		fmt.Fprintf(&line, ": %s", record.Err)
	}

	return line.String()
}
