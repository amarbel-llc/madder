setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/piv_agent.bash"
  export output
  start_piv_agent

  # A pigpen in its simplest form: one recipient per line. The recipient
  # is the virtual card's slot-9D key, so opening the store's sealed key
  # goes through the real piggy-agent.
  PIGPEN="$BATS_TEST_TMPDIR/piggy-ids"
  echo "$PIV_RECIPIENT_ID" >"$PIGPEN"
}

teardown() {
  stop_piv_agent
}

# bats file_tags=piv_agent

# Sealed-key stores (FDR 0011): `madder init -pigpen` mints an X25519
# store key and seals its secret half to the pigpen's recipients. Blobs
# are ordinary age files encrypted to the public half, so writing needs
# no agent; reading opens the sealed key once per process through the
# agent and decrypts every blob in software after that.

run_madder_agent() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

run_madder_no_agent() {
  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

init_sealed_store() {
  run_madder_agent init -pigpen "$PIGPEN" .sealed
  assert_success
}

# write_sealed_blob writes a one-line blob to .sealed with NO agent
# reachable and echoes its id.
write_sealed_blob() {
  local blob="$BATS_TEST_TMPDIR/blob-$RANDOM.txt"
  echo "$1" >"$blob"
  run_madder_no_agent write -format tap .sealed "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"
  [[ -n $blob_id ]] || fail "write returned no blob id. output: $output"
  echo "$blob_id"
}

sealed_store_dir() {
  echo ".madder/local/share/blob_stores/sealed"
}

card_ecdh_count() {
  grep -c 'GA ECDH 9D' "$PIV_FIBBY_LOG" || true
}

function init_pigpen_writes_no_secret { # @test
  init_sealed_store

  local config sidecar
  config="$(sealed_store_dir)/blob_store-config"
  sidecar="$(sealed_store_dir)/blob_store-key"
  [[ -f $config ]] || fail "no config at $config"
  [[ -f $sidecar ]] || fail "no sidecar at $sidecar"

  # The config is the sealed-key version and carries the PUBLIC key only.
  run cat "$config"
  assert_output --partial '! toml-blob_store_config-v5'
  assert_output --partial 'age_x25519_pub-'
  assert_output --partial 'holder = "process"'
  refute_output --partial 'age_x25519_sec'

  # The sidecar holds the sealed document and where its recipients came
  # from, and no secret in the clear either.
  run cat "$sidecar"
  assert_output --partial '! toml-blob_store_key-v1'
  assert_output --partial "$PIGPEN"
  assert_output --partial 'pigpen-v1'
  refute_output --partial 'age_x25519_sec'

  # Creating the store sealed the key in software: the card was not asked.
  [[ "$(card_ecdh_count)" == "0" ]] || fail "init used the card"
}

function sealed_store_writes_without_an_agent { # @test
  init_sealed_store

  write_sealed_blob "sealed-store-blob" >/dev/null

  local on_disk
  on_disk="$(find "$(sealed_store_dir)" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_disk ]] || fail "no blob file found"
  if grep -q 'sealed-store-blob' "$on_disk"; then
    fail "expected encrypted bytes on disk; found cleartext at $on_disk"
  fi

  run head -n 1 "$on_disk"
  assert_output 'age-encryption.org/v1'

  [[ "$(card_ecdh_count)" == "0" ]] || fail "writing used the card"
}

function sealed_store_reads_many_blobs_with_one_card_operation { # @test
  init_sealed_store

  local first second third
  first="$(write_sealed_blob "first sealed blob")"
  second="$(write_sealed_blob "second sealed blob")"
  third="$(write_sealed_blob "third sealed blob")"

  run_madder_agent cat .sealed "$first" "$second" "$third"
  assert_success
  assert_output --partial 'first sealed blob'
  assert_output --partial 'second sealed blob'
  assert_output --partial 'third sealed blob'

  # Three blobs, one process: the sealed key was opened once, so the
  # card did exactly one ECDH. This is the property the design exists
  # for (a 91,500-blob fsck must not be 91,500 card operations).
  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for 3 blobs, got $(card_ecdh_count)"
}

function sealed_store_fsck_verifies_through_the_agent { # @test
  init_sealed_store

  write_sealed_blob "fsck one" >/dev/null
  write_sealed_blob "fsck two" >/dev/null

  run_madder_agent fsck -format ndjson .sealed
  assert_success
  assert_output --partial '"state":"verified"'
  assert_output --partial 'blobs verified: 2'
  refute_output --partial 'blobs failed'

  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for the fsck, got $(card_ecdh_count)"
}

function sealed_store_without_an_agent_is_unreadable_not_missing { # @test
  init_sealed_store

  local blob_id
  blob_id="$(write_sealed_blob "needs the agent")"

  run_madder_no_agent cat .sealed "$blob_id"
  assert_failure
  refute_output --partial 'needs the agent'
  assert_output --partial 'no agent socket'
  assert_output --partial '1 blob(s) could not be read'
  refute_output --partial 'not found'

  run_madder_no_agent fsck -format ndjson .sealed
  assert_failure
  assert_output --partial '"state":"read_error"'
  refute_output --partial '"state":"corrupt"'
}

function init_pigpen_refuses_bad_input_and_creates_nothing { # @test
  # A pigpen that does not exist.
  run_madder_agent init -pigpen "$BATS_TEST_TMPDIR/no-such-pigpen" .sealed
  assert_failure
  [[ ! -e "$(sealed_store_dir)" ]] || fail "a store was created from a missing pigpen"

  # A pigpen naming no recipients: a key sealed to nobody could never be
  # opened.
  : >"$BATS_TEST_TMPDIR/empty-pigpen"
  run_madder_agent init -pigpen "$BATS_TEST_TMPDIR/empty-pigpen" .sealed
  assert_failure
  [[ ! -e "$(sealed_store_dir)" ]] || fail "a store was created from an empty pigpen"

  # -pigpen and -encryption are two different key schemes.
  run_madder_agent init -pigpen "$PIGPEN" -encryption generate .sealed
  assert_failure
  assert_output --partial 'cannot be combined with -encryption'
  [[ ! -e "$(sealed_store_dir)" ]] || fail "a store was created despite the conflict"
}

function init_pigpen_does_not_replace_an_existing_stores_key { # @test
  init_sealed_store

  local blob_id
  blob_id="$(write_sealed_blob "written under the first key")"

  local sidecar before
  sidecar="$(sealed_store_dir)/blob_store-key"
  before="$(cat "$sidecar")"

  # A second init must fail and must leave the sidecar alone: replacing
  # it would orphan every blob already written.
  run_madder_agent init -pigpen "$PIGPEN" .sealed
  assert_failure
  [[ "$(cat "$sidecar")" == "$before" ]] || fail "the existing sidecar was replaced"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'written under the first key'
}
