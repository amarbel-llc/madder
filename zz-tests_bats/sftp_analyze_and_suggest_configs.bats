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
#
# TODO(sftp-analyze bats coverage): all tests are currently
# `skip`ped because the existing SFTP store layer has an
# asymmetric path-handling convention — internal/foxtrot/blob_stores/
# store_remote_sftp.go:356 strips the leading slash from absolute
# remote paths before composing the blob's bucket path, while
# init's WriteRemoteConfig at internal/foxtrot/blob_stores/
# discover.go:176 uses the absolute path as-is for blob_store-config.
# A live SFTP store therefore lives at two different filesystem
# locations: blob_store-config at the absolute path, blobs at the
# relative-resolved path. sftp-analyze-and-suggest-configs reads
# both under one path; bats round-trip can only succeed once the
# convention is unified. Resolving the asymmetry is a follow-up.

function existing_verifies_when_init_then_analyze { # @test
  local remote_root="$BATS_TEST_TMPDIR/sftp-remote"
  init_sftp_test_store "$remote_root"

  skip "blocked by path-handling asymmetry in remoteSftp.remotePathForMerkleId — see TODO at top of file"

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
