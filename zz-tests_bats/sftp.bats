setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp.bash"
  export output
  start_sftp_server
}

teardown() {
  stop_sftp_server
}

# bats file_tags=net_cap

function sftp_handshake_exports_port_and_known_hosts { # @test
  [[ -n $SFTP_PORT ]] || fail "SFTP_PORT not exported"
  [[ -n $SFTP_KNOWN_HOSTS ]] || fail "SFTP_KNOWN_HOSTS not exported"
  [[ -f $SFTP_KNOWN_HOSTS ]] || fail "known_hosts file missing at $SFTP_KNOWN_HOSTS"
}
