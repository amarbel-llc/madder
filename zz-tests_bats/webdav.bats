setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/webdav.bash"
  export output
  start_webdav_server
}

teardown() {
  stop_webdav_server
}

# bats file_tags=net_cap

function webdav_init_bootstraps_remote_config { # @test
  # Fresh init must create the remote blob_store-config and round-trip
  # info-repo against it. Without the bootstrap step the very next
  # info-repo call would fail with "remote blob store config missing".
  init_webdav_test_store

  run_madder info-repo .webdav-test hash_type-id
  assert_success
  assert_line 'blake2b256'
}

function webdav_write_and_cat { # @test
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello webdav" >"$blob"

  run_madder write -format tap .webdav-test "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write returned empty hash. output: $output"

  run_madder cat .webdav-test "$hash"
  assert_success
  assert_output --partial "hello webdav"
}

function webdav_has_for_existing_blob { # @test
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "has test" >"$blob"

  run_madder write -format tap .webdav-test "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder has .webdav-test "$hash"
  assert_success
  assert_output --partial "found"
}

function webdav_list_after_writes { # @test
  init_webdav_test_store

  local blob1="$BATS_TEST_TMPDIR/blob1.txt"
  local blob2="$BATS_TEST_TMPDIR/blob2.txt"
  echo "list one" >"$blob1"
  echo "list two" >"$blob2"

  run_madder write -format tap .webdav-test "$blob1"
  assert_success
  run_madder write -format tap .webdav-test "$blob2"
  assert_success

  run_madder cat-ids .webdav-test
  assert_success
  assert_output
}

function webdav_fsck_clean { # @test
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "fsck content" >"$blob"
  run_madder write -format tap .webdav-test "$blob"
  assert_success

  run_madder fsck -format tap .webdav-test
  assert_success
  assert_output --partial "TAP version 14"
  refute_output --partial "not ok"
}

function webdav_concurrent_same_blob_writes { # @test
  # Two parallel writes of identical bytes. The duplicate-write
  # fallback in moveResource (HEAD-then-DELETE-temp on MOVE failure)
  # must let both invocations succeed without corruption.
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "concurrent same content" >"$blob"

  local pid1 pid2
  run_madder write -format tap .webdav-test "$blob" &
  pid1=$!
  run_madder write -format tap .webdav-test "$blob" &
  pid2=$!

  wait "$pid1"
  local rc1=$?
  wait "$pid2"
  local rc2=$?

  [[ $rc1 -eq 0 ]] || fail "first concurrent write failed: rc=$rc1"
  [[ $rc2 -eq 0 ]] || fail "second concurrent write failed: rc=$rc2"

  run_madder write -format tap .webdav-test "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder cat .webdav-test "$hash"
  assert_success
  assert_output --partial "concurrent same content"
}

function webdav_concurrent_writes_into_uncreated_bucket { # @test
  # Parallel writes of distinct blobs that share an uncreated bucket
  # parent. ensureCollection must absorb the MKCOL race: 405 (already
  # exists) plus PROPFIND-confirmed-collection means success.
  init_webdav_test_store

  local blob1="$BATS_TEST_TMPDIR/blob1.txt"
  local blob2="$BATS_TEST_TMPDIR/blob2.txt"
  echo "race blob one" >"$blob1"
  echo "race blob two" >"$blob2"

  local pid1 pid2
  run_madder write -format tap .webdav-test "$blob1" &
  pid1=$!
  run_madder write -format tap .webdav-test "$blob2" &
  pid2=$!

  wait "$pid1"
  local rc1=$?
  wait "$pid2"
  local rc2=$?

  [[ $rc1 -eq 0 ]] || fail "first race write failed: rc=$rc1"
  [[ $rc2 -eq 0 ]] || fail "second race write failed: rc=$rc2"
}

function webdav_duplicate_write_fallback { # @test
  # Sequential write of the same content twice; the second MOVE will
  # collide with an existing blob. The HEAD-on-MOVE-failure path must
  # not surface an error.
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "duplicate content" >"$blob"

  run_madder write -format tap .webdav-test "$blob"
  assert_success
  local hash1
  hash1="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder write -format tap .webdav-test "$blob"
  assert_success
  local hash2
  hash2="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  [[ $hash1 == "$hash2" ]] || fail "duplicate write produced different ids: $hash1 vs $hash2"
}

function webdav_empty_store_lists_no_blobs { # @test
  # Fresh init yields no blob-id lines from cat-ids. Filters out
  # debug stderr ('# (blob_store: ...) reading remote config ...')
  # and the trailing 'blobs with errors: N' summary; what's left
  # must be empty for an unpopulated store.
  init_webdav_test_store

  run_madder cat-ids .webdav-test
  assert_success
  local blob_lines
  blob_lines="$(echo "$output" | grep -v '^#' | grep -v '^blobs with errors:' | grep . || true)"
  [[ -z $blob_lines ]] || fail "fresh store yielded blob ids: $blob_lines"
}

function webdav_walker_filters_artifacts { # @test
  # After writes, cat-ids must emit exactly one blob-id line and
  # nothing resembling blob_store-config or tmp_ artifacts. The
  # debug prefix ('# (blob_store: ...)') legitimately mentions
  # blob_store-config; filter those out before pattern-matching.
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "walker filter" >"$blob"
  run_madder write -format tap .webdav-test "$blob"
  assert_success

  run_madder cat-ids .webdav-test
  assert_success
  local blob_lines
  blob_lines="$(echo "$output" | grep -v '^#' | grep -v '^blobs with errors:' | grep . || true)"
  local n
  n="$(echo "$blob_lines" | wc -l)"
  [[ $n -eq 1 ]] || fail "expected 1 blob line, got $n. lines:
$blob_lines"
  echo "$blob_lines" | grep -q '^blake2b256-' || fail "blob line not blake2b256-shaped: $blob_lines"
}

function webdav_init_url_without_trailing_slash_works { # @test
  # Both "http://host:port/path" and "http://host:port/path/" must
  # produce a working store. The URL normalization in makeWebdavStore
  # (trimRight trailing slash) is the unit; this test pins that init
  # plus round-trip works either way.
  run_madder init-webdav \
    -url "${WEBDAV_URL}store-no-slash" \
    .webdav-no-slash
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "no-trailing-slash" >"$blob"
  run_madder write -format tap .webdav-no-slash "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder cat .webdav-no-slash "$hash"
  assert_success
  assert_output --partial "no-trailing-slash"
}

function webdav_init_url_with_trailing_slash_works { # @test
  run_madder init-webdav \
    -url "${WEBDAV_URL}store-with-slash/" \
    .webdav-with-slash
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "with-trailing-slash" >"$blob"
  run_madder write -format tap .webdav-with-slash "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder cat .webdav-with-slash "$hash"
  assert_success
  assert_output --partial "with-trailing-slash"
}

function webdav_large_blob_round_trip { # @test
  # 10 MiB blob exercises chunked PUT (or the buffered analogue) and
  # the PROPFIND walker's getcontentlength parsing. Round-trip
  # confirms transport doesn't truncate or corrupt. Bypasses bats's
  # run-captures-output mechanism for the cat step because the
  # captured bytes contain NULs and exceed bash's variable-as-arg
  # limits in the subsequent cmp.
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/large.bin"
  head -c 10485760 /dev/urandom >"$blob"

  run_madder write -format tap .webdav-test "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  local outfile="$BATS_TEST_TMPDIR/large.out"
  local bin="${MADDER_BIN:-madder}"
  "$bin" cat .webdav-test "$hash" >"$outfile"
  cmp "$blob" "$outfile" || fail "10MiB blob did not round-trip cleanly"
}

function webdav_init_with_user_password { # @test
  # webdav.Dir from golang.org/x/net/webdav does not enforce auth, so
  # any user/password combination is accepted by the test server. The
  # actionable surface here is that the auth header is sent without
  # blowing up the request, which is what this scenario exercises.
  # Auth-rejection tests land once Phase 2's TLS plus bearer harness
  # ships.
  run_madder init-webdav \
    -url "${WEBDAV_URL}store-auth/" \
    -user testuser \
    -password testpass \
    .webdav-auth
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "auth roundtrip" >"$blob"

  run_madder write -format tap .webdav-auth "$blob"
  assert_success
}
