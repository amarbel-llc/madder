setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/piv_agent.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp.bash"
  export output
  start_piv_agent
  start_sftp_server

  PIGPEN="$BATS_TEST_TMPDIR/piggy-ids"
  echo "$PIV_RECIPIENT_ID" >"$PIGPEN"

  REMOTE_ROOT="$BATS_TEST_TMPDIR/sftp-remote"
}

teardown() {
  stop_sftp_server
  stop_piv_agent
}

# bats file_tags=piv_agent

# Sealed-key stores on an SFTP remote (FDR 0011, madder#296): the remote
# root holds the public-key config and the sealed blob_store-key sidecar,
# and no secret. Writing needs no agent; reading opens the sealed key once
# per process through the real piggy-agent.

run_madder_agent() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

run_madder_no_agent() {
  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

init_sftp_store() {
  local store_id="$1"
  shift
  run_madder_agent init-sftp-explicit \
    -host 127.0.0.1 \
    -port "$SFTP_PORT" \
    -user testuser \
    -password anything \
    -remote-path "$REMOTE_ROOT" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    "$@" \
    "$store_id"
}

init_sealed_sftp_store() {
  init_sftp_store .sealed-sftp -pigpen "$PIGPEN"
  assert_success
}

write_sealed_blob() {
  local blob="$BATS_TEST_TMPDIR/blob-$RANDOM.txt"
  echo "$1" >"$blob"
  run_madder_no_agent write -format tap .sealed-sftp "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"
  [[ -n $blob_id ]] || fail "write returned no blob id. output: $output"
  echo "$blob_id"
}

card_ecdh_count() {
  grep -c 'GA ECDH 9D' "$PIV_FIBBY_LOG" || true
}

function init_sftp_pigpen_puts_no_secret_on_the_remote { # @test
  init_sealed_sftp_store

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

  # madder#296: nothing under the remote root is a secret key.
  run grep -r 'age_x25519_sec' "$REMOTE_ROOT"
  assert_failure

  [[ "$(card_ecdh_count)" == "0" ]] || fail "init used the card"
}

function sealed_sftp_store_writes_without_an_agent { # @test
  init_sealed_sftp_store

  write_sealed_blob "sealed-sftp-blob" >/dev/null

  local on_remote
  on_remote="$(find "$REMOTE_ROOT" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_remote ]] || fail "no blob file on the remote"

  run head -n 1 "$on_remote"
  assert_output 'age-encryption.org/v1'

  [[ "$(card_ecdh_count)" == "0" ]] || fail "writing used the card"
}

function sealed_sftp_store_reads_many_blobs_with_one_card_operation { # @test
  init_sealed_sftp_store

  local first second third
  first="$(write_sealed_blob "first sealed sftp blob")"
  second="$(write_sealed_blob "second sealed sftp blob")"
  third="$(write_sealed_blob "third sealed sftp blob")"

  run_madder_agent cat .sealed-sftp "$first" "$second" "$third"
  assert_success
  assert_output --partial 'first sealed sftp blob'
  assert_output --partial 'second sealed sftp blob'
  assert_output --partial 'third sealed sftp blob'

  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for 3 blobs, got $(card_ecdh_count)"
}

function sealed_sftp_store_fsck_skips_the_sidecar { # @test
  init_sealed_sftp_store

  write_sealed_blob "fsck one" >/dev/null
  write_sealed_blob "fsck two" >/dev/null

  # The sidecar sits at the remote root next to the config; the blob walk
  # must not report it as a blob.
  run_madder_agent fsck -format ndjson .sealed-sftp
  assert_success
  assert_output --partial 'blobs verified: 2'
  refute_output --partial 'blobs failed'
  refute_output --partial 'blob_store-key'

  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for the fsck, got $(card_ecdh_count)"
}

function sealed_sftp_store_without_an_agent_is_unreadable { # @test
  init_sealed_sftp_store

  local blob_id
  blob_id="$(write_sealed_blob "needs the agent")"

  run_madder_no_agent cat .sealed-sftp "$blob_id"
  assert_failure
  refute_output --partial 'needs the agent'
  assert_output --partial '1 blob(s) could not be read'
}

function sync_into_a_sealed_sftp_store_needs_no_agent { # @test
  init_sealed_sftp_store

  run_madder init -encryption none .plain
  assert_success

  local blob="$BATS_TEST_TMPDIR/to-sync.txt"
  echo "synced into the sealed remote" >"$blob"
  run_madder write -format tap .plain "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"

  run_madder_no_agent sync -format ndjson .plain .sealed-sftp
  assert_success

  run_madder_agent cat .sealed-sftp "$blob_id"
  assert_success
  assert_output --partial 'synced into the sealed remote'
}

function init_sftp_pigpen_refuses_an_existing_remote { # @test
  # A plain store already lives at the remote root.
  init_sftp_store .plain-sftp
  assert_success

  local before
  before="$(cat "$REMOTE_ROOT/blob_store-config")"

  init_sftp_store .sealed-sftp -pigpen "$PIGPEN"
  assert_failure
  assert_output --partial '-pigpen needs a fresh remote'

  [[ "$(cat "$REMOTE_ROOT/blob_store-config")" == "$before" ]] ||
    fail "the existing remote config was replaced"
  [[ ! -e "$REMOTE_ROOT/blob_store-key" ]] || fail "a sidecar was written"
}

function init_sftp_pigpen_refuses_bad_input_and_creates_nothing { # @test
  init_sftp_store .sealed-sftp -pigpen "$BATS_TEST_TMPDIR/no-such-pigpen"
  assert_failure
  [[ ! -e $REMOTE_ROOT ]] || fail "a remote was created from a missing pigpen"

  init_sftp_store .sealed-sftp -pigpen "$PIGPEN" -encryption generate
  assert_failure
  assert_output --partial 'cannot be combined with -encryption'
  [[ ! -e $REMOTE_ROOT ]] || fail "a remote was created despite the conflict"
}
