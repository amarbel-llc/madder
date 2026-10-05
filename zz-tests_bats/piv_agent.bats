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

  # A second defect, pinned because it hides the first: the error names
  # neither decryption nor the agent. The agent replied without error, so
  # the unwrap failure is not an "agent error", and
  # blob_io.newFileReaderFromReadSeeker falls back to reading the blob as
  # if it were unencrypted. That feeds ciphertext to zstd, and the user is
  # told the blob does not exist.
  assert_output --partial 'failed to decompress: Unknown frame descriptor'
  assert_output --partial 'blob(s) not found'

  # The ECDH reached the card and succeeded, so the agent and the card
  # are not what failed.
  run grep 'GA ECDH 9D' "$PIV_FIBBY_LOG"
  assert_success
  assert_output --partial '9000'
}
