setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=tree_restore

# Per FDR 0001 (docs/features/0001-tree-restore.md), `tree-restore`
# materializes a captured tree from a receipt blob. Phase A only
# implements the precondition + sanitization checks; phase B adds
# per-type materialization.
#
# Phase A bats coverage corresponds to the RFC 0003 §Conformance
# Testing matrix rows that don't require materialization to fail:
#
#   - tree_restore_refuses_existing_destination
#   - tree_restore_refuses_path_escape_no_partial_writes
#   - tree_restore_refuses_nul_byte_in_path
#   - tree_restore_refuses_empty_root

# write_blob_id pipes content through `madder write -format tap` and
# echoes just the blob-id (the 4th column of the `ok 1 - ...` line).
# Used to inject a hand-crafted receipt blob without touching any
# tree-capture-specific layout.
write_blob_id() {
  local path="$1"
  run_madder write -format tap "$path"
  assert_success
  echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1
}

# capture_receipt_id captures the directory under PWD into the active
# store and echoes the receipt blob-id from the JSON sink record.
capture_receipt_id() {
  local dir="$1"
  run_madder tree-capture -format json "$dir"
  assert_success
  echo "$output" | grep -F '"type":"store_group_receipt"' \
    | sed -E 's/.*"receipt_id":"([^"]+)".*/\1/' | head -n 1
}

function tree_restore_refuses_existing_destination { # @test
  init_store

  mkdir src
  echo "x" >src/x.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  mkdir dest

  run_madder tree-restore "$rid" dest
  assert_failure
  echo "$output" | grep -qF 'destination already exists' \
    || fail "expected dest-exists refusal: $output"

  # No-side-effects symmetry with the injection-based scenarios: the
  # pre-existing dest should remain empty after the refusal.
  [[ -z "$(ls -A dest)" ]] || fail "dest not left empty after refusal"
}

# tree_restore_refuses_path_escape_no_partial_writes asserts a
# hand-crafted receipt with a `path` field that escapes the
# destination is refused, AND the destination is not created.
function tree_restore_refuses_path_escape_no_partial_writes { # @test
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/escape-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! madder-tree_capture-receipt-v1
	---

	{"path":"../../../etc/passwd","root":"src","type":"file","mode":"0644","size":1,"blob_id":"blake2b256-x"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_failure
  echo "$output" | grep -qF 'entry escapes destination' \
    || fail "expected escape refusal: $output"

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal; found it"
}

function tree_restore_refuses_nul_byte_in_path { # @test
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/nul-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! madder-tree_capture-receipt-v1
	---

	{"path":"foo\u0000bar","root":"src","type":"file","mode":"0644","size":1,"blob_id":"blake2b256-x"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_failure
  echo "$output" | grep -qF 'NUL byte' \
    || fail "expected NUL-byte refusal: $output"

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}

function tree_restore_refuses_empty_root { # @test
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/empty-root-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! madder-tree_capture-receipt-v1
	---

	{"path":"foo","root":"","type":"file","mode":"0644","size":1,"blob_id":"blake2b256-x"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_failure
  echo "$output" | grep -qF 'empty root' \
    || fail "expected empty-root refusal: $output"

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}
