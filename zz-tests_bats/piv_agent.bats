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

# run_madder's 2s timeout is too tight for a card round trip through the
# agent (dewey's client lists keys and retries once on failure).
run_madder_piv() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

function piv_recipient_store_writes_without_the_card { # @test

  # Encrypting to a pivy_ecdh_p256_pub recipient is pure software: the
  # card is not consulted. So the write must succeed, the bytes on disk
  # must not be cleartext, and fibby must have seen no ECDH.
  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "piv-recipient-roundtrip" >"$blob"

  run_madder_piv write -format tap .piv "$blob"
  assert_success

  local on_disk
  on_disk="$(find .madder -type f -path '*/blake2b256/*' -print -quit)"
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

function piv_recipient_store_cannot_read_through_piggy_agent { # @test

  # CHARACTERIZATION TEST — pins a defect, not a contract.
  #
  # A store encrypted to a single PIV recipient cannot be read back
  # through the real piggy-agent. The card does its part: fibby's wire
  # log shows the slot-9D ECDH answered 9000. The failure is client
  # side, in dewey's pivy ECDH client, which madder reaches via
  # markl.Id.GetIOWrapper(): it skips one byte of the agent's reply and
  # takes the first SSH string as the shared secret, but piggy-agent
  # answers SSH_AGENT_EXTENSION_RESPONSE followed by the extension NAME
  # and only then the secret. The Go unit test
  # TestPivyRecipient_CannotDecrypt_ExtensionResponseFraming shows the
  # same client fails on exactly that framing and round-trips on the
  # other.
  #
  # The fix belongs in dewey (purse-first). When it lands this test
  # should FAIL: flip it to assert the cat succeeds and prints the blob.

  run_madder_piv init -encryption "$PIV_RECIPIENT_ID" .piv
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "piv-recipient-roundtrip" >"$blob"
  local blob_id
  run_madder_piv write -format tap .piv "$blob"
  assert_success
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"
  [[ -n $blob_id ]] || fail "write returned no blob id. output: $output"

  run_madder_piv cat .piv "$blob_id"
  assert_failure
  refute_output --partial 'piv-recipient-roundtrip'

  # Regression guard for madder#297: the failure must be reported as a
  # decryption failure. It used to fall back to an unencrypted read,
  # which fed ciphertext to zstd and told the user "failed to
  # decompress" and "blob(s) not found". Keep this when the test above is
  # flipped — move it to a case that still cannot decrypt.
  assert_output --partial 'blob is encrypted but could not be decrypted'
  assert_output --partial '1 blob(s) could not be read'
  refute_output --partial 'failed to decompress'
  refute_output --partial 'not found'

  # The ECDH reached the card and succeeded, so the agent and the card
  # are not what failed.
  run grep 'GA ECDH 9D' "$PIV_FIBBY_LOG"
  assert_success
  assert_output --partial '9000'
}
