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
# Single-root captures encode Root="." (RFC 0003 §Root Encoding),
# so `restore <rid> out` lands files directly under `out/` (no per-
# root prefix), and `diff <rid> src` (against the originally-
# captured directory) is symmetric with `diff <rid> out` (against
# the post-restore tree). Phase B-D tests use the post-restore
# `out/` form; Phase E asserts the capture-symmetric form.

# write_blob_id and capture_receipt_id live in lib/common.bash.

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
  assert_output --partial 'directory does not exist'
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
  assert_output --partial 'not a directory'
}

function diff_refuses_unparseable_receipt_id { # @test
  init_store

  mkdir dir
  run_cg diff "garbage-not-a-markl-id" dir
  assert_failure
  assert_output --partial 'parse receipt-id'
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
  assert_output --partial 'entry escapes destination'
}

function diff_accepts_dot_destination { # @test
  # Regression for `pathConfinedTo` with dest=`.`: filepath.Clean
  # strips the `./` prefix from the materialized path, so the old
  # HasPrefix(materialized, dest+sep) check rejected every benign
  # entry. The fix uses filepath.Rel.
  #
  # We only assert the validate-phase guard does not fire. The
  # tree-vs-receipt diff itself is noisy here (cwd holds the store
  # and other test files), so we don't assert success — only that
  # the `entry escapes destination` refusal is gone.
  init_store

  mkdir src
  echo "x" >src/x.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg diff "$rid" .
  refute_output --partial 'entry escapes destination'
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
  # Refute only the M/A/D/T diff-entry lines.
  refute_line --regexp '^[MADT]  '
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
  assert_line --regexp '^A  extra\.txt'
  assert_output --partial 'tree differs from receipt'
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

  rm out/b.txt

  run_cg diff "$rid" out
  assert_failure
  assert_line --regexp '^D  b\.txt'
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

  echo "modified" >out/file.txt

  run_cg diff "$rid" out
  assert_failure
  assert_line --regexp '^M  file\.txt.*blob '
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

  chmod 0600 out/file.txt

  run_cg diff "$rid" out
  assert_failure
  assert_line --regexp '^M  file\.txt.*mode '
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

  rm out/file
  ln -s other out/file

  run_cg diff "$rid" out
  assert_failure
  assert_line --regexp '^T  file.*file -> symlink'
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

  rm out/link
  ln -s b.txt out/link

  run_cg diff "$rid" out
  assert_failure
  assert_line --regexp '^M  link.*target.*a\.txt.*->.*b\.txt'
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
  refute_line --regexp '^[BMADT]  '
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
  assert_line --regexp '^B  src/file\.txt.*blob .* missing in source store'
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
  refute_line --regexp '^B  '
  assert_line --regexp '^D  src/file\.txt'
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

  echo "modified" >out/a.txt       # M  a.txt
  rm out/b.txt                     # D  b.txt
  echo "extra" >out/extra.txt      # A  extra.txt

  run_cg diff "$rid" out
  assert_failure
  assert_line --regexp '^M  a\.txt.*blob '
  assert_line --regexp '^D  b\.txt'
  assert_line --regexp '^A  extra\.txt'
  assert_output --partial 'tree differs from receipt: 3 entries'
}

# Phase E: capture-diff symmetry.
#
# `cg capture <dir>` and `cg diff <rid> <dir>` are symmetric for
# single-root captures: the same <dir> argument that produced the
# receipt diffs clean against it without an intermediate `restore`.
# This is enabled by RFC 0003 §Root Encoding, which collapses Root
# to "." for single-root capture-groups so the receipt key
# (filepath.Join(Root, Path)) reduces to Path — matching the disk-
# walk's rel-to-<dir> key.

function diff_is_clean_against_originally_captured_dir { # @test
  # capture src → diff $rid src → expected: no differences, exit 0.
  init_store

  mkdir src
  echo "x" >src/x.txt
  mkdir -p src/inner
  echo "nested" >src/inner/n.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg diff "$rid" src
  assert_success
  refute_line --regexp '^[MADT]  '
}

function diff_is_clean_when_run_from_captured_dir_with_dot { # @test
  # capture src; cd src; diff $rid . → expected: no differences, exit 0.
  # Blocked on amarbel-llc/madder#145 (CWD-relative blob_stores
  # discovery doesn't walk up to ancestor `.madder/`). Re-enable
  # once #145 lands.
  skip "blocked on #145 (subdir store discovery)"

  init_store

  mkdir src
  echo "x" >src/x.txt
  mkdir -p src/inner
  echo "nested" >src/inner/n.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  cd src
  run_cg diff "$rid" .
  assert_success
  refute_line --regexp '^[MADT]  '
}
