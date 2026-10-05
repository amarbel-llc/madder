setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/piv_agent.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/webdav.bash"
  export output
  start_piv_agent
  start_webdav_server

  PIGPEN="$BATS_TEST_TMPDIR/piggy-ids"
  echo "$PIV_RECIPIENT_ID" >"$PIGPEN"

  REMOTE_ROOT="$WEBDAV_ROOT/sealed"
}

teardown() {
  stop_webdav_server
  stop_piv_agent
}

# bats file_tags=piv_agent

# Sealed-key stores on a WebDAV remote (FDR 0011, madder#296): the base
# URL holds the public-key config and the sealed blob_store-key sidecar,
# and no secret. Mirrors pigpen_sftp.bats.

run_madder_agent() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

run_madder_no_agent() {
  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

init_webdav_store() {
  local store_id="$1"
  shift
  run_madder_agent init-webdav -url "${WEBDAV_URL}sealed/" "$@" "$store_id"
}

init_sealed_webdav_store() {
  init_webdav_store .sealed-webdav -pigpen "$PIGPEN"
  assert_success
}

write_sealed_blob() {
  local blob="$BATS_TEST_TMPDIR/blob-$RANDOM.txt"
  echo "$1" >"$blob"
  run_madder_no_agent write -format tap .sealed-webdav "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"
  [[ -n $blob_id ]] || fail "write returned no blob id. output: $output"
  echo "$blob_id"
}

card_ecdh_count() {
  grep -c 'GA ECDH 9D' "$PIV_FIBBY_LOG" || true
}

function init_webdav_pigpen_puts_no_secret_on_the_remote { # @test
  init_sealed_webdav_store

  local config="$REMOTE_ROOT/blob_store-config"
  local sidecar="$REMOTE_ROOT/blob_store-key"
  [[ -f $config ]] || fail "no remote config at $config"
  [[ -f $sidecar ]] || fail "no remote sidecar at $sidecar"

  run cat "$config"
  assert_output --partial '! toml-blob_store_config-v5'
  assert_output --partial 'age_x25519_pub-'
  assert_output --partial 'holder = "process"'

  run cat "$sidecar"
  assert_output --partial '! toml-blob_store_key-v1'
  assert_output --partial 'pigpen-v1'

  run grep -r 'age_x25519_sec' "$REMOTE_ROOT"
  assert_failure

  [[ "$(card_ecdh_count)" == "0" ]] || fail "init used the card"
}

function sealed_webdav_store_reads_many_blobs_with_one_card_operation { # @test
  init_sealed_webdav_store

  local first second third
  first="$(write_sealed_blob "first sealed webdav blob")"
  second="$(write_sealed_blob "second sealed webdav blob")"
  third="$(write_sealed_blob "third sealed webdav blob")"

  [[ "$(card_ecdh_count)" == "0" ]] || fail "writing used the card"

  run_madder_agent cat .sealed-webdav "$first" "$second" "$third"
  assert_success
  assert_output --partial 'first sealed webdav blob'
  assert_output --partial 'second sealed webdav blob'
  assert_output --partial 'third sealed webdav blob'

  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for 3 blobs, got $(card_ecdh_count)"
}

function sealed_webdav_store_fsck_skips_the_sidecar { # @test
  init_sealed_webdav_store

  write_sealed_blob "fsck one" >/dev/null
  write_sealed_blob "fsck two" >/dev/null

  run_madder_agent fsck -format ndjson .sealed-webdav
  assert_success
  assert_output --partial 'blobs verified: 2'
  refute_output --partial 'blobs failed'
  refute_output --partial 'blob_store-key'
}

function sealed_webdav_store_without_an_agent_is_unreadable { # @test
  init_sealed_webdav_store

  local blob_id
  blob_id="$(write_sealed_blob "needs the agent")"

  run_madder_no_agent cat .sealed-webdav "$blob_id"
  assert_failure
  refute_output --partial 'needs the agent'
  assert_output --partial '1 blob(s) could not be read'
}

function key_reseal_replaces_the_sidecar_on_the_webdav_remote { # @test
  init_sealed_webdav_store

  local blob_id before
  blob_id="$(write_sealed_blob "written before the webdav reseal")"
  before="$(cat "$REMOTE_ROOT/blob_store-key")"

  run_madder_no_agent key-status .sealed-webdav
  assert_success
  assert_output --partial 'status:      in sync'

  run_madder_agent key-reseal .sealed-webdav
  assert_success
  assert_output --partial 're-sealed .sealed-webdav to 1 recipient ('

  [[ "$(cat "$REMOTE_ROOT/blob_store-key")" != "$before" ]] ||
    fail "the remote sidecar was not replaced"

  run_madder_agent cat .sealed-webdav "$blob_id"
  assert_success
  assert_output --partial 'written before the webdav reseal'
}

function init_webdav_pigpen_refuses_an_existing_remote { # @test
  init_webdav_store .plain-webdav
  assert_success

  local before
  before="$(cat "$REMOTE_ROOT/blob_store-config")"

  init_webdav_store .sealed-webdav -pigpen "$PIGPEN"
  assert_failure

  [[ "$(cat "$REMOTE_ROOT/blob_store-config")" == "$before" ]] ||
    fail "the existing remote config was replaced"
  [[ ! -e "$REMOTE_ROOT/blob_store-key" ]] || fail "a sidecar was written"
}

function init_webdav_pigpen_refuses_bad_input_and_creates_nothing { # @test
  init_webdav_store .sealed-webdav -pigpen "$BATS_TEST_TMPDIR/no-such-pigpen"
  assert_failure
  [[ ! -e $REMOTE_ROOT ]] || fail "a remote was created from a missing pigpen"

  init_webdav_store .sealed-webdav -pigpen "$PIGPEN" -encryption generate
  assert_failure
  assert_output --partial 'cannot be combined with -encryption'
  [[ ! -e $REMOTE_ROOT ]] || fail "a remote was created despite the conflict"
}
