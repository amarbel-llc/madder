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

# write_blob_id pipes a file through `madder write -format tap` and
# echoes just the blob-id (the 4th column of the `ok 1 - ...` line).
# Used to inject a hand-crafted receipt blob without touching any
# tree-capture-specific layout. Pass an explicit store-id as the first
# arg to target a non-default store: `write_blob_id .work path`.
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

# capture_receipt_id captures the directory under PWD into the active
# store and echoes the receipt blob-id from the JSON sink record.
capture_receipt_id() {
  local dir="$1"
  run_madder tree-capture -format json "$dir"
  assert_success
  echo "$output" | grep -F '"type":"store_group_receipt"' |
    sed -E 's/.*"receipt_id":"([^"]+)".*/\1/' | head -n 1
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
  echo "$output" | grep -qF 'destination already exists' ||
    fail "expected dest-exists refusal: $output"

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
  echo "$output" | grep -qF 'entry escapes destination' ||
    fail "expected escape refusal: $output"

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
  echo "$output" | grep -qF 'NUL byte' ||
    fail "expected NUL-byte refusal: $output"

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
  echo "$output" | grep -qF 'empty root' ||
    fail "expected empty-root refusal: $output"

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}

# Phase B: per-type materialization round-trips.
# Each scenario captures a tree with a specific entry type, restores
# it into a fresh dest, and asserts the materialized layout matches
# the captured one byte-for-byte.

function tree_restore_round_trips_file { # @test
  init_store

  mkdir src
  printf 'hello\nworld\n' >src/greeting.txt
  chmod 0644 src/greeting.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_madder tree-restore "$rid" out
  assert_success

  [[ -f out/src/greeting.txt ]] ||
    fail "expected out/src/greeting.txt to exist"

  diff src/greeting.txt out/src/greeting.txt ||
    fail "restored content differs from captured"

  local mode
  mode="$(file_mode out/src/greeting.txt)"
  [[ $mode == '644' ]] || fail "expected mode 644 on restored file; got $mode"
}

function tree_restore_round_trips_dir { # @test
  init_store

  mkdir -p src/inner/deeper
  echo "x" >src/inner/deeper/x.txt
  # Non-default mode: 0o755 is the default umask-modulated MkdirAll
  # mode, so testing against it can't distinguish a captured-mode
  # restore from a fresh MkdirAll. 0o750 forces the capture-side mode
  # bits to flow through.
  chmod 0750 src/inner

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_madder tree-restore "$rid" out
  assert_success

  [[ -d out/src/inner ]] || fail "expected out/src/inner to be a dir"
  [[ -d out/src/inner/deeper ]] || fail "expected out/src/inner/deeper to be a dir"
  [[ -f out/src/inner/deeper/x.txt ]] || fail "expected nested file to exist"

  local mode
  mode="$(file_mode out/src/inner)"
  [[ $mode == '750' ]] || fail "expected mode 750 on restored dir; got $mode"
}

function tree_restore_skips_type_other_with_notice { # @test
  # RFC 0003 §Consumer Rules §Per-Type Materialization: entries of
  # type "other" (devices, fifos, sockets) are skipped with a notice.
  # Inject a hand-crafted receipt with a type:"other" entry so the
  # test doesn't depend on tree-capture's ability to capture
  # non-regular files in the test environment.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/other-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! madder-tree_capture-receipt-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
	{"path":"fifo","root":"src","type":"other","mode":"0600"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_success
  echo "$output" | grep -qF 'skipping entry of type "other"' ||
    fail "expected skip notice for type:other: $output"

  [[ -d out/src ]] || fail "expected out/src dir to be created"
  [[ ! -e out/src/fifo ]] || fail "expected out/src/fifo NOT to exist"
}

function tree_restore_round_trips_symlink { # @test
  init_store

  mkdir src
  echo "target content" >src/target.txt
  ln -s target.txt src/link

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_madder tree-restore "$rid" out
  assert_success

  [[ -L out/src/link ]] || fail "expected out/src/link to be a symlink"

  local target
  target="$(readlink out/src/link)"
  [[ $target == 'target.txt' ]] ||
    fail "expected symlink target 'target.txt', got '$target'"

  # The link resolves through the restored target, so reading it gives
  # the captured content.
  diff src/target.txt out/src/link ||
    fail "symlink-resolved content differs from captured target"
}

# Phase C: RFC 0003 §Store-Hint Resolution branches.

function tree_restore_uses_hint_store_when_config_matches { # @test
  # Branch 2: hint present, store configured locally, config-hash
  # matches → silent auto-use.
  init_store
  run_madder init -encryption none .work
  assert_success

  mkdir src
  echo "x" >src/x.txt

  run_madder tree-capture -format json .work src
  assert_success
  local rid
  rid="$(echo "$output" | grep -F '"type":"store_group_receipt"' |
    sed -E 's/.*"receipt_id":"([^"]+)".*/\1/' | head -n 1)"
  [[ -n $rid ]] || fail "no receipt id"

  # Restore must NOT emit any of the hint-resolution notices when the
  # match path fires.
  run_madder tree-restore "$rid" out
  assert_success
  refute_output --partial 'falling back to active store'
  refute_output --partial 'no store hint'
  refute_output --partial 'has been re-configured'

  [[ -f out/src/x.txt ]] || fail "expected restored file"
}

function tree_restore_warns_on_config_drift { # @test
  # Branch 3: hint present, store configured, but the config-hash in
  # the hint does NOT match the local store's current config-hash.
  # Synthesize a receipt whose hint claims a stale digest.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/drift-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	- store/.default < blake2b256-stalehashstalehashstalehashstalehashstalehashstalehashstalehashstale
	! madder-tree_capture-receipt-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_failure
  echo "$output" | grep -qF 'has been re-configured since this receipt was written' ||
    fail "expected drift warning: $output"
  echo "$output" | grep -qF 'pass -store' ||
    fail "expected -store override hint: $output"

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}

function tree_restore_falls_back_to_active_store_on_missing_hint { # @test
  # Branch 4: hint names a store that is not configured locally.
  # Synthesize a receipt naming a store-id that was never init'd.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/missing-store-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	- store/.never_configured < blake2b256-arbitraryhasharbitraryhasharbitraryhasharbitraryhasharbitr
	! madder-tree_capture-receipt-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_success
  echo "$output" | grep -qF 'is not configured locally' ||
    fail "expected missing-store notice: $output"
  echo "$output" | grep -qF 'falling back to active store' ||
    fail "expected fallback notice: $output"

  [[ -d out/src ]] || fail "expected out/src to be created via fallback"
}

function tree_restore_falls_back_to_active_store_on_no_hint { # @test
  # Branch 5: receipt carries no `- store/...` line.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/no-hint-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! madder-tree_capture-receipt-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_madder tree-restore "$rid" out
  assert_success
  echo "$output" | grep -qF 'no store hint' ||
    fail "expected no-hint notice: $output"
  echo "$output" | grep -qF 'falling back to active store' ||
    fail "expected fallback notice: $output"

  [[ -d out/src ]] || fail "expected out/src to be created via fallback"
}

function tree_restore_store_flag_overrides_hint { # @test
  # Branch 1: -store flag wins over hint resolution. We synthesize a
  # receipt whose hint would trigger drift (branch 3) AND pass -store
  # to point at a configured store; the override must suppress the
  # drift refusal and proceed silently.
  init_store
  run_madder init -encryption none .work
  assert_success

  # Write y.txt as a blob into .work so the entry's blob_id
  # actually resolves there. Use the tap output to capture the id.
  mkdir src
  echo "y" >src/y.txt
  local blob_id
  blob_id="$(write_blob_id .work src/y.txt)"
  [[ -n $blob_id ]] || fail "write returned empty blob id"

  # Synthesize a receipt with a stale hint pointing at .work and
  # one file entry whose blob_id is resolvable in .work.
  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/override-receipt"
  cat >"$receipt_path" <<RECEIPT
---
- store/.work < blake2b256-stalehashstalehashstalehashstalehashstalehashstalehashstalehashstale
! madder-tree_capture-receipt-v1
---

{"path":".","root":"src","type":"dir","mode":"0755"}
{"path":"y.txt","root":"src","type":"file","mode":"0644","size":2,"blob_id":"$blob_id"}
RECEIPT

  # The receipt itself must live in .work because phase 1 of
  # tree-restore fetches the receipt against the resolved store —
  # `-store .work` makes phase 1 read from .work.
  local rid
  rid="$(write_blob_id .work "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  # Without -store, this would trigger branch 3 (drift refusal).
  # With -store, it must succeed silently — no drift warning, no
  # fallback notice.
  run_madder tree-restore -store .work "$rid" out
  assert_success
  refute_output --partial 'has been re-configured'
  refute_output --partial 'falling back'

  [[ -f out/src/y.txt ]] || fail "expected restored file via -store override"
}
