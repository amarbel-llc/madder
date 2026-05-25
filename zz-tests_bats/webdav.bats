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

# ADR-0005 parity scenarios: each one has a direct SFTP analogue
# (sftp_init_idempotent_fails, sftp_init_compression_default,
# sftp_init_hash_type_id_default, sftp_info_repo_host_stays_local,
# sftp_info_repo_config_immutable_encodes_remote in sftp.bats).
# The WebDAV store must surface the same authoritative-config
# semantics. The on-disk zstd-magic check from
# sftp_write_compresses_per_remote_config is tracked separately in
# issue #187 — it needs the test server's tmpdir exposed via the
# RFC 0001 handshake, which doesn't fit a focused-test commit.

function webdav_init_idempotent_local_fails { # @test
  # Second init for the same local store id must fail; the local
  # pointer config already exists. The remote bootstrap is
  # idempotent on its own (HEAD-then-skip), but the local config
  # write is not.
  init_webdav_test_store

  run_madder init-webdav \
    -url "${WEBDAV_URL}store-1/" \
    .webdav-test
  assert_failure
}

function webdav_info_repo_compression_type_from_remote { # @test
  # Per ADR 0005, info-repo MUST surface the remote TomlV3's
  # compression-type rather than anything from the local
  # TomlWebDAVV0 transport config. The bootstrap config uses
  # "zstd"; a regression that falls back to the local zero value
  # would surface "" here.
  init_webdav_test_store

  run_madder info-repo .webdav-test compression-type
  assert_success
  assert_line 'zstd'
}

function webdav_info_repo_hash_type_id_from_remote { # @test
  # Sibling of webdav_info_repo_compression_type_from_remote:
  # hash_type-id is also a blob-store-property, not transport, so
  # it must resolve through the remote blob_store-config.
  init_webdav_test_store

  run_madder info-repo .webdav-test hash_type-id
  assert_success
  assert_line 'blake2b256'
}

function webdav_info_repo_url_stays_local { # @test
  # Reading a transport-only key (url) on a WebDAV store MUST NOT
  # open an HTTP connection. Backend-property keys (compression-type,
  # hash_type-id, encryption) do; transport keys (url, user) do not.
  # Parallels sftp_info_repo_host_stays_local.
  init_webdav_test_store

  run_madder info-repo .webdav-test url
  assert_success
  assert_output --partial "$WEBDAV_URL"
  refute_output --partial 'reading remote config'
}

function webdav_info_repo_config_immutable_no_password { # @test
  # End-to-end counterpart to TestConfigKeyValues_WebDAVRedactsSecrets:
  # info-repo's config-immutable dump must not leak the password
  # (or any other secret). Catches a regression where a sibling
  # field re-serialises the whole struct or where the redaction
  # branch is bypassed in a future refactor.
  init_webdav_test_store "store-creds" .webdav-creds \
    -user testuser -password sekret-do-not-leak

  run_madder info-repo .webdav-creds config-immutable
  assert_success
  refute_output --partial 'password'
  refute_output --partial 'sekret-do-not-leak'
}

function webdav_write_compresses_per_remote_config { # @test
  # Per ADR 0005, the remote blob_store-config dictates on-wire
  # shape. init_webdav_test_store provisions zstd, so a published
  # blob's bytes MUST start with the zstd magic (28b52ffd). If the
  # IO wrapper falls back to compression-none, the on-disk bytes
  # equal the plaintext and this fails. Parallels
  # sftp_write_compresses_per_remote_config.
  #
  # Closes #187 — the test depends on WEBDAV_ROOT being exposed via
  # the RFC 0001 handshake metadata.
  init_webdav_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'compress me please' >"$blob"

  run_madder write .webdav-test "$blob"
  assert_success

  local on_disk
  on_disk="$(find "$WEBDAV_ROOT" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_disk ]] || fail "no blob file found under $WEBDAV_ROOT"

  local magic
  magic="$(xxd -p -l 4 "$on_disk")"
  [[ $magic == '28b52ffd' ]] || fail "expected zstd magic at start of $on_disk; got $magic"
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
  # blowing up the request — config plumbing + applyWebdavAuth basic
  # branch.
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

function webdav_init_with_bearer_token { # @test
  # webdav.Dir does not enforce bearer auth either, so this scenario
  # exercises only the client-side plumbing — that
  # 'Authorization: Bearer <t>' is set without breaking the request
  # and that the basic-auth branch is skipped when a bearer token is
  # configured.
  run_madder init-webdav \
    -url "${WEBDAV_URL}store-bearer/" \
    -bearer-token "test-bearer-eyJhbGc.payload.sig" \
    .webdav-bearer
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "bearer roundtrip" >"$blob"

  run_madder write -format tap .webdav-bearer "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder cat .webdav-bearer "$hash"
  assert_success
  assert_output --partial "bearer roundtrip"
}

function webdav_init_rejects_multiple_auth_modes { # @test
  # validateWebdavAuth (called from makeWebdavStore) refuses to build
  # a store when more than one of {password, bearer-token,
  # tls-client-cert-path} is set. The CLI surface here is that the
  # error reaches the user with an actionable message rather than a
  # confusing "unexpected status 401" later.
  #
  # Note: init-webdav writes the local config and then calls into
  # ensureWebdavRemoteConfigExists -> makeWebdavStore (indirectly via
  # the factory). The validation fires before any HTTP request, so
  # the failure is fast and the local config remains absent.
  run_madder init-webdav \
    -url "${WEBDAV_URL}store-multi-auth/" \
    -password testpass \
    -bearer-token testbearer \
    .webdav-multi-auth
  assert_failure
  assert_output --partial 'at most one of'
}

function webdav_https_round_trip_anonymous { # @test
  # Swap the default plaintext setup for a TLS server. With
  # -tls-ca-path pinned to the server's self-signed cert, the
  # client-side cert verification succeeds; without it, the handshake
  # would fail because Go's default CA bundle doesn't include the
  # test cert.
  stop_webdav_server
  start_webdav_server_tls

  run_madder init-webdav \
    -url "${WEBDAV_URL}store-tls-anon/" \
    -tls-ca-path "$WEBDAV_CERT_PATH" \
    .webdav-tls-anon
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "tls anonymous roundtrip" >"$blob"

  run_madder write -format tap .webdav-tls-anon "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder cat .webdav-tls-anon "$hash"
  assert_success
  assert_output --partial "tls anonymous roundtrip"
}

function webdav_https_round_trip_with_basic_auth { # @test
  # Combine TLS transport with basic auth credentials on the client
  # side. The test server doesn't actually validate the credentials,
  # so this confirms applyWebdavAuth's basic-branch + TLS-client
  # construction co-exist without breaking the request.
  stop_webdav_server
  start_webdav_server_tls

  run_madder init-webdav \
    -url "${WEBDAV_URL}store-tls-basic/" \
    -tls-ca-path "$WEBDAV_CERT_PATH" \
    -user tlsuser \
    -password tlspass \
    .webdav-tls-basic
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "tls + basic roundtrip" >"$blob"

  run_madder write -format tap .webdav-tls-basic "$blob"
  assert_success
}

function webdav_init_with_tls_client_cert { # @test
  # Generates a self-signed client cert + key with openssl, then
  # init-webdav with -tls-client-cert-path + -tls-client-key-path.
  # The test server accepts any client cert (no ClientCAs configured
  # via webdav.Dir), so this scenario verifies the client-side
  # plumbing — tls.LoadX509KeyPair runs, the http.Transport's
  # TLSClientConfig.Certificates is populated, and a write+cat
  # round-trips through the TLS handshake.
  stop_webdav_server
  start_webdav_server_tls

  local client_key="$BATS_TEST_TMPDIR/client.key"
  local client_crt="$BATS_TEST_TMPDIR/client.crt"
  openssl req -x509 -newkey ed25519 \
    -keyout "$client_key" -out "$client_crt" \
    -days 1 -nodes -subj '/CN=test-client' >/dev/null 2>&1 ||
    fail "openssl could not generate a self-signed client cert+key pair"

  run_madder init-webdav \
    -url "${WEBDAV_URL}store-tls-cert/" \
    -tls-ca-path "$WEBDAV_CERT_PATH" \
    -tls-client-cert-path "$client_crt" \
    -tls-client-key-path "$client_key" \
    .webdav-tls-cert
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "tls client-cert roundtrip" >"$blob"

  run_madder write -format tap .webdav-tls-cert "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder cat .webdav-tls-cert "$hash"
  assert_success
  assert_output --partial "tls client-cert roundtrip"
}
