setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=diff

# `cutting-garden diff <receipt-id> <dir>` walks <dir> and compares
# its contents against the entries in the receipt. Output is one
# line per difference: M (mode/blob/target), A (on disk only),
# D (in receipt only), T (type changed). Exit 0 when the tree
# matches; non-zero on any difference or error.
#
# Tests use the symmetric round-trip pattern: capture src → restore
# out → diff $rid out. The dir arg to diff is "out" (the
# materialization destination), so receipt entries with Root="src"
# materialize to "src/..." under out/, and diff's keys align.

# capture_receipt_id captures the directory under PWD into the active
# store and echoes the receipt blob-id from the JSON sink record.
capture_receipt_id() {
  local dir="$1"
  run_cg capture -format json "$dir"
  assert_success
  echo "$output" | grep -F '"type":"store_group_receipt"' |
    sed -E 's/.*"receipt_id":"([^"]+)".*/\1/' | head -n 1
}

# write_blob_id pipes a file through `madder write -format tap` and
# echoes just the blob-id (the 4th column of the `ok 1 - ...` line).
write_blob_id() {
  local store path
  if [[ $# -eq 1 ]]; then
    path="$1"
    run_madder write -format tap "$path"
  else
    store="$1"
    path="$2"
    run_madder write -format tap "$store" "$path"
  fi
  assert_success
  echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1
}

# Phase A: refusals.

function diff_refuses_nonexistent_dir { # @test
  init_store

  mkdir src
  echo "x" >src/x.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg diff "$rid" no-such-dir
  assert_failure
  echo "$output" | grep -qF 'directory does not exist' ||
    fail "expected nonexistent-dir refusal: $output"
}

function diff_refuses_dir_arg_that_is_a_file { # @test
  init_store

  mkdir src
  echo "x" >src/x.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  echo "data" >regular-file.txt

  run_cg diff "$rid" regular-file.txt
  assert_failure
  echo "$output" | grep -qF 'not a directory' ||
    fail "expected not-a-directory refusal: $output"
}

function diff_refuses_unparseable_receipt_id { # @test
  init_store

  mkdir dir
  run_cg diff "garbage-not-a-markl-id" dir
  assert_failure
  echo "$output" | grep -qF 'parse receipt-id' ||
    fail "expected parse-receipt-id refusal: $output"
}

function diff_refuses_path_escape { # @test
  # Inherits restore's RFC 0003 §Consumer Rules §Path Sanitization
  # via the shared validateEntries helper. Diff doesn't write
  # anything, but a receipt with an escape path is still ill-formed
  # and we refuse it before walking.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/escape-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":"../../../etc/passwd","root":"src","type":"file","mode":"0644","size":1,"blob_id":"blake2b256-x"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  mkdir target
  run_cg diff "$rid" target
  assert_failure
  echo "$output" | grep -qF 'entry escapes destination' ||
    fail "expected escape refusal: $output"
}

# Phase B: happy path.

function diff_is_clean_after_round_trip { # @test
  # capture src → restore out → diff out → no differences, exit 0.
  # Covers the most common workflow: prove a restored tree faithfully
  # represents what was captured.
  init_store

  mkdir src
  printf 'hello\nworld\n' >src/greeting.txt
  chmod 0644 src/greeting.txt
  mkdir -p src/inner
  chmod 0750 src/inner
  echo "nested" >src/inner/n.txt
  ln -s greeting.txt src/link

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  run_cg diff "$rid" out
  assert_success
  # `output` captures stdout AND stderr; the source-store-hint
  # notices are stderr noise common to every default-store diff.
  # Assert only that no M/A/D/T lines (the actual diff entries)
  # were emitted.
  local diff_lines
  diff_lines="$(echo "$output" | grep -E '^[MADT]  ' || true)"
  [[ -z $diff_lines ]] ||
    fail "expected zero diff entries, got: $diff_lines"
}

# Phase C: drift detection.
#
# Each test follows: capture src → restore out → perturb out →
# diff out → expect non-zero exit and the perturbation reported.

function diff_detects_added_file { # @test
  init_store

  mkdir src
  echo "a" >src/a.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  echo "extra" >out/extra.txt

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^A  extra.txt' ||
    fail "expected 'A  extra.txt' line: $output"
  echo "$output" | grep -qF 'tree differs from receipt' ||
    fail "expected tree-differs error: $output"
}

function diff_detects_deleted_file { # @test
  init_store

  mkdir src
  echo "a" >src/a.txt
  echo "b" >src/b.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  rm out/src/b.txt

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^D  src/b.txt' ||
    fail "expected 'D  src/b.txt' line: $output"
}

function diff_detects_modified_file_content { # @test
  init_store

  mkdir src
  echo "original" >src/file.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  echo "modified" >out/src/file.txt

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^M  src/file.txt.*blob ' ||
    fail "expected 'M  src/file.txt  blob ...' line: $output"
}

function diff_detects_modified_file_mode { # @test
  init_store

  mkdir src
  echo "x" >src/file.txt
  chmod 0644 src/file.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  chmod 0600 out/src/file.txt

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^M  src/file.txt.*mode ' ||
    fail "expected 'M  src/file.txt  mode ...' line: $output"
}

function diff_detects_type_change { # @test
  # Capture file, replace materialized file with a symlink. Diff
  # should report 'T file -> symlink'.
  init_store

  mkdir src
  echo "content" >src/file
  echo "target" >src/other
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  rm out/src/file
  ln -s other out/src/file

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^T  src/file.*file -> symlink' ||
    fail "expected 'T  src/file  file -> symlink' line: $output"
}

function diff_detects_symlink_target_change { # @test
  init_store

  mkdir src
  echo "first" >src/a.txt
  echo "second" >src/b.txt
  ln -s a.txt src/link
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  rm out/src/link
  ln -s b.txt out/src/link

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^M  src/link.*target.*a.txt.*->.*b.txt' ||
    fail "expected 'M  src/link  target ...' line: $output"
}

# Phase D: -verify-blobs-exist (receipt-vs-store check).
#
# Default diff is tree-vs-receipt: the on-disk content is hashed and
# compared to the receipt's records. -verify-blobs-exist adds the
# orthogonal probe — does the source store actually hold the blobs
# the receipt references? Catches receipts with gc'd blobs or
# hand-crafted bogus ids.

function diff_verify_blobs_exist_clean_round_trip { # @test
  # Round-trip with the flag: every receipt blob was just written to
  # the store, so HasBlob is true for all of them; no B lines.
  init_store

  mkdir src
  echo "x" >src/x.txt
  mkdir -p src/sub
  echo "y" >src/sub/y.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  run_cg diff -verify-blobs-exist "$rid" out
  assert_success
  local diff_lines
  diff_lines="$(echo "$output" | grep -E '^[BMADT]  ' || true)"
  [[ -z $diff_lines ]] ||
    fail "expected zero diff entries with flag, got: $diff_lines"
}

function diff_verify_blobs_exist_detects_missing_blob { # @test
  # Hand-craft a receipt referencing a blob no store holds. With
  # the flag set, diff emits a B line for the missing blob (in
  # addition to the D lines for the absent on-disk paths).
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/missing-blob-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
	{"path":"file.txt","root":"src","type":"file","mode":"0644","size":5,"blob_id":"blake2b256-deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  mkdir target

  run_cg diff -verify-blobs-exist "$rid" target
  assert_failure
  echo "$output" | grep -qE '^B  src/file.txt.*blob .* missing in source store' ||
    fail "expected 'B  src/file.txt blob ... missing' line: $output"
}

function diff_without_flag_does_not_emit_B_lines { # @test
  # Same hand-crafted receipt as above. Without the flag, the
  # missing blob goes unreported — only the D lines for absent
  # on-disk paths surface.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/missing-blob-receipt-2"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
	{"path":"file.txt","root":"src","type":"file","mode":"0644","size":5,"blob_id":"blake2b256-deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  mkdir target

  run_cg diff "$rid" target
  assert_failure
  echo "$output" | grep -qE '^B  ' &&
    fail "expected no B lines without flag, got: $output"
  echo "$output" | grep -qE '^D  src/file.txt' ||
    fail "expected D lines for absent paths: $output"
}

function diff_reports_multiple_differences_sorted_by_path { # @test
  # Combination test: every difference type at once. Output must be
  # sorted by path so test assertions can rely on ordering.
  init_store

  mkdir src
  echo "a" >src/a.txt
  echo "b" >src/b.txt
  echo "c" >src/c.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  echo "modified" >out/src/a.txt   # M  src/a.txt
  rm out/src/b.txt                 # D  src/b.txt
  echo "extra" >out/extra.txt      # A  extra.txt

  run_cg diff "$rid" out
  assert_failure
  echo "$output" | grep -qE '^M  src/a.txt.*blob ' ||
    fail "expected M src/a.txt: $output"
  echo "$output" | grep -qE '^D  src/b.txt' ||
    fail "expected D src/b.txt: $output"
  echo "$output" | grep -qE '^A  extra.txt' ||
    fail "expected A extra.txt: $output"
  echo "$output" | grep -qF 'tree differs from receipt: 3 entries' ||
    fail "expected '3 entries' summary: $output"
}
