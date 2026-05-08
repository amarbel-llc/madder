setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp_legacy.bash"
  export output
  start_sftp_server
  start_test_ssh_agent
}

teardown() {
  stop_test_ssh_agent
  stop_sftp_server
}

# bats file_tags=net_cap

function existing_verifies_when_init_then_analyze { # @test
  local remote_root="$BATS_TEST_TMPDIR/sftp-remote"
  init_sftp_test_store "$remote_root"

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"
  run_madder write -format tap .sftp-test "$blob"
  assert_success

  local tmpdir="$BATS_TEST_TMPDIR/suggest"
  mkdir -p "$tmpdir"

  TMPDIR="$tmpdir" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host "testuser@127.0.0.1:$SFTP_PORT" \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS"
  assert_success
  assert_output --partial 'ok 1 - existing'
}
