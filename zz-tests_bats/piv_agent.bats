setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/piv_agent.bash"
  export output
  start_piv_agent
}

teardown() {
  stop_piv_agent
}

# bats file_tags=piv_agent

# These tests run madder against the REAL Rust piggy-agent over fibby,
# piggy's virtual PIV card. They exist because the single-PIV-recipient
# path had no end-to-end coverage and was broken: with dewey's ECDH
# client (piggy before 78ce2ef) a store could be written but never read
# back through piggy-agent, since that client mis-parsed the agent's
# reply. The pivy_ecdh_p256_pub wrapper now uses piggy's own client.

# run_madder's 2s timeout is too tight for a card round trip through the
# agent.
run_madder_piv() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

# piv_write_blob writes a one-line blob to .piv and echoes its id.
piv_write_blob() {
  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "$1" >"$blob"
  run_madder_piv write -format tap .piv "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"
  [[ -n $blob_id ]] || fail "write returned no blob id. output: $output"
  echo "$blob_id"
}

piv_on_disk_blob() {
  find .madder -type f -path '*/blake2b256/*' -print -quit
}

function piv_recipient_store_writes_without_the_card { # @test

  # Encrypting to a pivy_ecdh_p256_pub recipient is pure software: the
  # card is not consulted. So the write must succeed, the bytes on disk
  # must not be cleartext, and fibby must have seen no ECDH.
  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  piv_write_blob "piv-recipient-roundtrip" >/dev/null

  local on_disk
  on_disk="$(piv_on_disk_blob)"
  [[ -n $on_disk ]] || fail "no blob file found under .madder"
  if grep -q 'piv-recipient-roundtrip' "$on_disk"; then
    fail "expected encrypted bytes on disk; found cleartext at $on_disk"
  fi

  # The blob is an age file. blob_io relies on this header to tell an
  # encrypted blob from a cleartext one (madder#297).
  run head -n 1 "$on_disk"
  assert_output 'age-encryption.org/v1'

  run grep -c 'GA ECDH 9D' "$PIV_FIBBY_LOG"
  assert_output '0'
}

function piv_recipient_store_writes_with_no_agent_socket { # @test

  # Writing needs no agent at all, so it must work with every agent
  # socket variable unset. Before piggy 78ce2ef the wrapper resolved the
  # socket when it was built, and init/write failed here.
  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "written-with-no-agent" >"$blob"

  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" \
    write -format tap .piv "$blob"
  assert_success
}

function piv_recipient_store_round_trips_through_piggy_agent { # @test

  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  local blob_id
  blob_id="$(piv_write_blob "piv-recipient-roundtrip")"

  run_madder_piv cat .piv "$blob_id"
  assert_success
  assert_output --partial 'piv-recipient-roundtrip'

  # The read went through the card: one slot-9D ECDH, answered 9000.
  run grep 'GA ECDH 9D' "$PIV_FIBBY_LOG"
  assert_success
  assert_output --partial '9000'
}

function piv_recipient_store_reports_agent_failure_as_unreadable { # @test

  # With no agent reachable the blob is still there; it just cannot be
  # decrypted. That must not be reported as a missing blob (madder#297).
  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  local blob_id
  blob_id="$(piv_write_blob "piv-recipient-roundtrip")"

  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" \
    cat .piv "$blob_id"
  assert_failure
  refute_output --partial 'piv-recipient-roundtrip'
  assert_output --partial '1 blob(s) could not be read'
  refute_output --partial 'not found'
  refute_output --partial 'failed to decompress'
}

function piv_recipient_store_reports_undecryptable_blob { # @test

  # Regression guard for madder#297's reader half. A blob that is
  # encrypted but cannot be decrypted, for a reason that is NOT an agent
  # failure, used to fall back to an unencrypted read: ciphertext went to
  # zstd and the user was told "failed to decompress" and "blob(s) not
  # found". Here the agent and card work, and the blob's age header MAC
  # is corrupted on disk, so the unwrap succeeds and the header check
  # fails.
  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  local blob_id
  blob_id="$(piv_write_blob "piv-recipient-roundtrip")"

  local on_disk
  on_disk="$(piv_on_disk_blob)"
  [[ -n $on_disk ]] || fail "no blob file found under .madder"

  # The age header ends with a `--- <base64 mac>` line. Change the MAC's
  # first character to a different one.
  chmod u+w "$on_disk"
  LC_ALL=C sed -i -e '0,/^--- ./{s/^--- A/--- B/;t;s/^--- ./--- A/}' "$on_disk"

  run_madder_piv cat .piv "$blob_id"
  assert_failure
  refute_output --partial 'piv-recipient-roundtrip'
  assert_output --partial 'blob is encrypted but could not be decrypted'
  assert_output --partial '1 blob(s) could not be read'
  refute_output --partial 'failed to decompress'
  refute_output --partial 'not found'
}
