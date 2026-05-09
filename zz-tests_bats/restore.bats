setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=restore

# Per FDR 0001 (docs/features/0001-restore.md), `restore`
# materializes a captured tree from a receipt blob. Phase A only
# implements the precondition + sanitization checks; phase B adds
# per-type materialization.

function restore_refuses_existing_destination { # @test
  init_store

  mkdir src
  echo "x" >src/x.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  mkdir dest

  run_cg restore "$rid" dest
  assert_failure
  assert_output --partial 'destination already exists'

  # No-side-effects symmetry with the injection-based scenarios.
  [[ -z "$(ls -A dest)" ]] || fail "dest not left empty after refusal"
}

function restore_refuses_path_escape_no_partial_writes { # @test
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

  run_cg restore "$rid" out
  assert_failure
  assert_output --partial 'entry escapes destination'

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal; found it"
}

function restore_refuses_nul_byte_in_path { # @test
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/nul-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":"foo\u0000bar","root":"src","type":"file","mode":"0644","size":1,"blob_id":"blake2b256-x"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore "$rid" out
  assert_failure
  assert_output --partial 'NUL byte'

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}

function restore_refuses_empty_root { # @test
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/empty-root-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":"foo","root":"","type":"file","mode":"0644","size":1,"blob_id":"blake2b256-x"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore "$rid" out
  assert_failure
  assert_output --partial 'empty root'

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}

# Phase B: per-type materialization round-trips.

function restore_round_trips_file { # @test
  init_store

  mkdir src
  printf 'hello\nworld\n' >src/greeting.txt
  chmod 0644 src/greeting.txt

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  [[ -f out/greeting.txt ]] ||
    fail "expected out/greeting.txt to exist"

  diff src/greeting.txt out/greeting.txt ||
    fail "restored content differs from captured"

  local mode
  mode="$(file_mode out/greeting.txt)"
  [[ $mode == '644' ]] || fail "expected mode 644 on restored file; got $mode"
}

function restore_round_trips_dir { # @test
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

  run_cg restore "$rid" out
  assert_success

  [[ -d out/inner ]] || fail "expected out/inner to be a dir"
  [[ -d out/inner/deeper ]] || fail "expected out/inner/deeper to be a dir"
  [[ -f out/inner/deeper/x.txt ]] || fail "expected nested file to exist"

  local mode
  mode="$(file_mode out/inner)"
  [[ $mode == '750' ]] || fail "expected mode 750 on restored dir; got $mode"
}

function restore_skips_type_other_with_notice { # @test
  # RFC 0003 §Consumer Rules §Per-Type Materialization: entries of
  # type "other" (devices, fifos, sockets) are skipped with a notice.
  # Inject a hand-crafted receipt so the test doesn't depend on
  # capture's ability to capture non-regular files in the test env.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/other-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
	{"path":"fifo","root":"src","type":"other","mode":"0600"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore "$rid" out
  assert_success
  assert_output --partial 'skipping entry of type "other"'

  [[ -d out/src ]] || fail "expected out/src dir to be created"
  [[ ! -e out/src/fifo ]] || fail "expected out/src/fifo NOT to exist"
}

function restore_round_trips_symlink { # @test
  init_store

  mkdir src
  echo "target content" >src/target.txt
  ln -s target.txt src/link

  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success

  [[ -L out/link ]] || fail "expected out/link to be a symlink"

  local target
  target="$(readlink out/link)"
  [[ $target == 'target.txt' ]] ||
    fail "expected symlink target 'target.txt', got '$target'"

  # The link resolves through the restored target.
  diff src/target.txt out/link ||
    fail "symlink-resolved content differs from captured target"
}

# Phase C: RFC 0003 §Store-Hint Resolution branches.

function restore_uses_hint_store_when_config_matches { # @test
  # Branch 2: hint present, store configured locally, config-hash
  # matches → silent auto-use.
  init_store
  run_madder init -encryption none .work
  assert_success

  mkdir src
  echo "x" >src/x.txt

  run_cg capture -format json .work src
  assert_success
  local rid
  rid="$(echo "$output" | grep -F '"type":"store_group_receipt"' |
    sed -E 's/.*"receipt_id":"([^"]+)".*/\1/' | head -n 1)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success
  refute_output --partial 'falling back to active store'
  refute_output --partial 'no store hint'
  refute_output --partial 'has been re-configured'

  [[ -f out/x.txt ]] || fail "expected restored file"
}

function restore_uses_hint_store_when_default_store_emits_hint { # @test
  # Branch 2 for the default-store path. Per #92 option (c), default-
  # store captures now emit a hint pointing at the resolved id
  # (e.g. ".default"); restore must consume it silently rather than
  # fall through the no-hint branch.
  init_store

  mkdir src
  echo "x" >src/x.txt
  local rid
  rid="$(capture_receipt_id src)"
  [[ -n $rid ]] || fail "no receipt id"

  run_cg restore "$rid" out
  assert_success
  refute_output --partial 'falling back to active store'
  refute_output --partial 'no store hint'
  refute_output --partial 'has been re-configured'

  [[ -f out/x.txt ]] || fail "expected restored file"
}

function restore_warns_on_config_drift { # @test
  # Branch 3: hint present, store configured, but the config-hash in
  # the hint does NOT match the local store's current config-hash.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/drift-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	- store/.default < blake2b256-stalehashstalehashstalehashstalehashstalehashstalehashstalehashstale
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore "$rid" out
  assert_failure
  assert_output --partial 'has been re-configured since this receipt was written'
  assert_output --partial 'pass -store'

  [[ ! -e out ]] || fail "expected out/ not to exist after refusal"
}

function restore_falls_back_to_active_store_on_missing_hint { # @test
  # Branch 4: hint names a store that is not configured locally.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/missing-store-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	- store/.never_configured < blake2b256-arbitraryhasharbitraryhasharbitraryhasharbitraryhasharbitr
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore "$rid" out
  assert_success
  assert_output --partial 'is not configured locally'
  assert_output --partial 'falling back to active store'

  [[ -d out/src ]] || fail "expected out/src to be created via fallback"
}

function restore_falls_back_to_active_store_on_no_hint { # @test
  # Branch 5: receipt carries no `- store/...` line.
  init_store

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/no-hint-receipt"
  cat >"$receipt_path" <<-'RECEIPT'
	---
	! cutting_garden-capture_receipt-fs-v1
	---

	{"path":".","root":"src","type":"dir","mode":"0755"}
RECEIPT

  local rid
  rid="$(write_blob_id "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore "$rid" out
  assert_success
  assert_output --partial 'no store hint'
  assert_output --partial 'falling back to active store'

  [[ -d out/src ]] || fail "expected out/src to be created via fallback"
}

function restore_store_flag_overrides_hint { # @test
  # Branch 1: -store flag wins over hint resolution. The receipt's
  # hint would trigger drift (branch 3); -store must suppress the
  # drift refusal and proceed silently.
  init_store
  run_madder init -encryption none .work
  assert_success

  mkdir src
  echo "y" >src/y.txt
  local blob_id
  blob_id="$(write_blob_id .work src/y.txt)"
  [[ -n $blob_id ]] || fail "write returned empty blob id"

  local receipt_path
  receipt_path="$BATS_TEST_TMPDIR/override-receipt"
  cat >"$receipt_path" <<RECEIPT
---
- store/.work < blake2b256-stalehashstalehashstalehashstalehashstalehashstalehashstalehashstale
! cutting_garden-capture_receipt-fs-v1
---

{"path":".","root":"src","type":"dir","mode":"0755"}
{"path":"y.txt","root":"src","type":"file","mode":"0644","size":2,"blob_id":"$blob_id"}
RECEIPT

  # The receipt must live in .work because phase 1 of restore fetches
  # the receipt against the resolved store; `-store .work` makes
  # phase 1 read from .work.
  local rid
  rid="$(write_blob_id .work "$receipt_path")"
  [[ -n $rid ]] || fail "write returned empty hash"

  run_cg restore -store .work "$rid" out
  assert_success
  refute_output --partial 'has been re-configured'
  refute_output --partial 'falling back'

  [[ -f out/src/y.txt ]] || fail "expected restored file via -store override"
}
