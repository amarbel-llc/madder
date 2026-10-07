setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=sync

function cross_hash_sync { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "cross-hash-test" >"$blob"
  local blake_sha
  blake_sha="$(write_blob_id "$blob")"

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success

  run_madder cat-ids .sha256
  assert_success
  assert_output --partial "$blake_sha"

  run_madder cat .sha256 "$blake_sha"
  assert_success
  assert_line "cross-hash-test"
}

function cross_hash_sync_into_single_hash_dest { # @test

  # CHARACTERIZATION TEST — pins today's defective behavior, not the
  # desired behavior. Do not read these assertions as a contract.
  #
  # madder-sync(1) documents: "When source and destination use
  # different hash types, blobs are rehashed (source digests are not
  # preserved in single-hash destinations)" — promising that a
  # single-hash destination still ACCEPTS the sync and merely loses the
  # mapping. It does not. localHashBucketed's
  # AddForeignBlobDigestForNativeDigest errors for a single-hash store
  # rather than no-oping (store_local_hash_bucketed.go:293), and
  # copy.go:129 treats that error as fatal to the copy. So every blob
  # FAILS, and the man page's "not preserved" is really "not
  # transferred".
  #
  # The command used to exit 0 with zero blobs transferred; that half is
  # fixed (madder#299) and asserted below. What remains of #286 is that
  # the destination is not refused up front and the blob is written
  # anyway.
  #
  # The sibling cross_hash_sync covers the MULTI-hash destination,
  # where the alias is registered and the source digest stays
  # resolvable. This is the uncovered half.
  #
  # Tracked as madder#286. Expect this test to FAIL once single-hash
  # destinations are refused up front (#284 phase 2). That is the
  # intended fix, not a regression — rewrite these assertions then.

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "single-hash-dest-test" >"$blob"
  local source_id
  source_id="$(write_blob_id "$blob")"

  run_madder init -hash_type-id sha256 -encryption none .sha256single
  assert_success

  # `single_hash` has no init flag — it is only set by a hand-written
  # config or by sftp discovery of a legacy tree — so flip the config
  # directly and re-pin its FDR-0008 digest.
  local config=".madder/local/share/blob_stores/sha256single/blob_store-config"
  [[ -f $config ]] || fail "expected store config at $config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  printf 'single_hash = true\n' >>"$config"
  chmod 0444 "$config"

  run_madder config-pin_digest .sha256single
  assert_success

  run grep -E '^single_hash = true$' "$config"
  assert_success # the flip survived re-pinning

  run_madder sync -format ndjson .default .sha256single

  # The defect. Part (1) used to be "exit 0": a migration script checking
  # only the status saw success. madder#299 fixed that half — any failed
  # blob now fails the command — so this asserts the fix.
  assert_failure
  assert_output --partial 'sync: 1 blob(s) failed'
  assert_output --partial 'Successes: 0, Failures: 1' # (2) nothing transferred
  # (3) and the per-blob record carries the single-hash rejection.
  assert_output --partial '"state":"failed"'
  assert_output --partial 'single-hash store does not support foreign digest mapping'
  refute_output --partial '"state":"transferred"'

  # (4) The nastiest part: the blob IS written. copy.go commits the
  # writer before the alias step, so the rehashed sha256 blob lands and
  # only the mapping fails — yet the record says "failed" and the
  # summary counts zero successes. A retry cannot distinguish "never
  # transferred" from "transferred, unmappable".
  run_madder cat-ids .sha256single
  assert_success
  assert_output --partial 'sha256-'    # the rehashed blob is present ...
  refute_output --partial "$source_id" # ... but the source digest does not resolve

  # Regression guard for madder#287, fixed: enumerating a single-hash
  # local store used to parse the store's own `blob_store-config` as a
  # blob id, so every listing carried "blobs with errors: 2". Refuting a
  # NONZERO count rather than the whole phrase, so this holds whether
  # the summary line is printed with a zero or dropped entirely. This is
  # the only coverage of that fix — if the assertions above are rewritten
  # when #286 lands, keep this one (or rehome it).
  refute_output --regexp 'blobs with errors: [1-9]'
}

function sync_cross_hash_reports_dest_id { # @test

  # madder#285: a rehashed blob's record carries the destination digest
  # alongside the source one, so a migration can read the
  # sha256<->blake2b map straight off the sync stream.
  #
  # The second pass is the resume case. The blob is skipped as already
  # present (size -1) before any writer exists, so its dest_id has to be
  # read back from the alias the first pass left — and must equal the one
  # the first pass reported, or a resumed run's map would disagree with
  # a fresh run's.

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "dest-id-test" >"$blob"
  local source_id
  source_id="$(write_blob_id "$blob")"

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --regexp "\"id\":\"$source_id\",\"dest_id\":\"sha256-[a-z0-9]+\",\"size\":[0-9]+,\"state\":\"transferred\""

  # The dest_id is a real digest in the destination: it resolves to the
  # same bytes.
  local dest_id
  dest_id="$(grep -o '"dest_id":"[^"]*"' <<<"$output" | cut -d'"' -f4)"
  run_madder cat .sha256 "$dest_id"
  assert_success
  assert_line 'dest-id-test'

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --partial "\"id\":\"$source_id\",\"dest_id\":\"$dest_id\",\"size\":-1,\"state\":\"transferred\""
}

function sync_carries_aliases_across_without_copying_the_bytes_again { # @test

  # A store that was the destination of a cross-hash sync holds each blob
  # once, plus an alias under the source's hash type. Syncing THAT store
  # onward to one with the same default hash type used to store every
  # alias as a second full copy of the bytes under the alias's hash type,
  # doubling the destination. It must carry the alias across as an alias.

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "stored once" >"$blob"
  local source_id
  source_id="$(write_blob_id "$blob")"

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success
  run_madder sync -format ndjson .default .sha256
  assert_success
  local native_id
  native_id="$(grep -o '"dest_id":"[^"]*"' <<<"$output" | cut -d'"' -f4)"
  [[ -n $native_id ]] || fail "the cross-hash sync reported no dest_id"

  run_madder init -hash_type-id sha256 -encryption none .onward
  assert_success

  run_madder sync -format ndjson .sha256 .onward
  assert_success
  # The alias id is reported with the native digest it points at ...
  assert_output --regexp "\"id\":\"$source_id\",\"dest_id\":\"$native_id\",\"size\":[0-9-]+,\"state\":\"transferred\""
  refute_output --partial '"state":"failed"'

  # ... and the destination holds the bytes once: one blob file, one link.
  local onward=".madder/local/share/blob_stores/onward"
  local files links
  files="$(find "$onward" -type f ! -name 'blob_store-*' | wc -l | tr -d ' ')"
  links="$(find "$onward" -type l | wc -l | tr -d ' ')"
  [[ $files == 1 ]] || fail "expected 1 blob file in .onward, found $files"
  [[ $links == 1 ]] || fail "expected 1 alias link in .onward, found $links"

  # Both names read the blob.
  run_madder cat .onward "$source_id"
  assert_success
  assert_line 'stored once'
  run_madder cat .onward "$native_id"
  assert_success
  assert_line 'stored once'

  # A second pass finds both present and still reports the mapping.
  run_madder sync -format ndjson .sha256 .onward
  assert_success
  assert_output --partial "\"id\":\"$source_id\",\"dest_id\":\"$native_id\",\"size\":-1,\"state\":\"transferred\""
}

function sync_same_hash_omits_dest_id { # @test

  # No rehash, so nothing to map: dest_id would only repeat id.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "same-hash-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder init -encryption none .other
  assert_success

  run_madder sync -format ndjson .default .other
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial '"dest_id"'
}

function sync_idempotent { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "idempotent-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
}

function sync_already_present_blob_reports_negative_size { # @test

  # An already-present blob folds into a "transferred" record in the
  # legacy ndjson stream rather than getting a state of its own, so the
  # only thing separating a skip from a real transfer is the size: the
  # HasBlob early return (copy.go:25-29) sets bytesWritten = -1, and
  # blob_transfers/main.go:171 emits that result unconditionally, so
  # the -1 reaches the record.
  #
  # Pinned because consumers currently have no other per-record way to
  # tell the two apart, and because the comment at sync.go:626 claimed
  # this folds in with size 0. Treat it as an accidental signal, not a
  # designed contract — madder#285 tracks giving the skip its own
  # "exists" state, and this test should be rewritten when that lands
  # rather than preserved.
  #
  # The second pass also demonstrates why a cross-hash re-sync is
  # resumable at all: HasBlob is called with the SOURCE digest, and it
  # resolves because the first pass left a foreign-digest alias behind.

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "skip-size-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial '"size":-1' # a real copy reports real bytes

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --partial '"size":-1' # nothing copied the second time
}

function sync_crap_already_present_blob_reports_done_not_skipped { # @test

  # The crap path does NOT distinguish an already-present blob either,
  # contrary to what sync.go's comments and GetDescription().Long used
  # to claim. op.Skip (sync.go:406) is gated on IsErrBlobAlreadyExists,
  # but the ordinary "destination already has it" case returns a nil
  # error from ImportBlobIfNecessary — copy.go:25-29 records a state,
  # not an error — so it takes the else branch and is reported as an
  # ordinary item.
  #
  # Observed on the second pass: state "done", operation_end
  # "skipped":0, and bytes -1. So syncStateExists is unreachable for
  # this case on this path, and `bytes`/`size` == -1 is the ONLY signal
  # separating a skip from a real transfer in either output format.
  # madder#285 tracks fixing that; rewrite this test when it lands.

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "crap-skip-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256 # crap by default under `run`
  assert_success

  run_madder sync .default .sha256
  assert_success
  assert_output --partial '"state":"done"'
  assert_output --partial '"bytes":-1'
  assert_output --partial '"skipped":0'
  refute_output --partial '"exists"'
}

function fsck_verifies_foreign_digest_aliases_but_double_counts { # @test

  # A cross-hash sync into a multi-hash destination leaves a
  # foreign-digest alias — a relative symlink at the source digest's
  # path pointing at the native blob — so a store holding one real blob
  # enumerates two entries that both look like blobs.
  #
  # What fsck does with that was unverified (madder#291). Answer, in two
  # parts:
  #
  # 1. It is SAFE. Both entries verify. VerifyBlob reads the bytes and
  #    compares against the expected digest, and the same bytes hash
  #    correctly under BOTH algorithms — a digest is a function of bytes
  #    AND hash type, so one blob legitimately has a valid sha256 and a
  #    valid blake2b256 id. No false corruption reports, so a green fsck
  #    over an aliased store means what it appears to mean.
  #
  # 2. It DOUBLE-COUNTS. Both the blob count and the byte total include
  #    the alias, so they roughly double for a fully-rehashed store. The
  #    16-byte blob below is reported as 2 blobs / 32 B.
  #
  # Part 2 is the defect worth fixing; part 1 is why it is cosmetic
  # rather than dangerous. Asserting the inflated numbers rather than
  # the correct ones because they are what madder does today — rewrite
  # this when #291 is fixed.

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "fsck-alias-test" >"$blob" # 16 bytes including the newline
  local source_id
  source_id="$(write_blob_id "$blob")"

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success

  # -format ndjson explicitly: the piped default already resolves to it
  # (sftp_fsck_json_auto_detects covers that), but this test is about
  # fsck's counts, so it should not also depend on format detection.
  run_madder fsck -format ndjson .sha256
  assert_success

  # The alias verifies under the source hash ...
  assert_output --partial "\"id\":\"$source_id\",\"store\":\".sha256\",\"state\":\"verified\""
  # ... and the native blob under the destination hash.
  assert_output --regexp '"id":"sha256-[a-z0-9]+","store":"\.sha256","state":"verified"'
  refute_output --partial '"state":"failed"'

  # One blob on disk, counted twice, and its bytes counted twice.
  assert_output --partial 'blobs verified: 2, bytes verified: 32 B'
}

function sync_crap_auto_detects { # @test

  # Default auto-format under `run` (no TTY) must emit ndjson-crap.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-crap-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync .default .sha256
  assert_success
  # Meta header (Source: "madder") still emits a "crap" record; the body is
  # now operation-family records (scan phase + Operation), not a result
  # summary.
  assert_output --partial '"type":"crap"'
  assert_output --partial '"type":"operation_start"'
  assert_output --partial '"type":"operation_end"'
  refute_output --partial 'TAP version 14'
}

function sync_ndjson_opt_out { # @test

  # -format ndjson keeps the legacy {id,state,size,error} records.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sync-ndjson-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format ndjson .default .sha256
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial '"type":"crap"'
}

function sync_rejects_tap { # @test

  # sync no longer supports TAP; -format tap is rejected at runtime.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "x" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder init -hash_type-id sha256 -encryption none .sha256
  assert_success

  run_madder sync -format tap .default .sha256
  assert_failure
  assert_output --partial "does not support -format tap"
}
