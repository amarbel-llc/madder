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

today_sftp_log() {
  local date
  date="$(date -u +%Y-%m-%d)"
  echo "$XDG_LOG_HOME/madder/blob-writes-$date.ndjson"
}

function sftp_write_emits_written_record { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  # Per `madder write`: a leading blob-store-id arg switches the active
  # store for subsequent file args.
  run_madder write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_log)"
  [[ -s $log ]] || fail "expected write-log at $log, got none"

  local n
  n="$(grep -c '"op":"written"' "$log" || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n. log:
$(cat "$log")"
}

function sftp_write_disabled_by_no_write_log_flag { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  # --no-write-log is a global flag; place it before the subcommand.
  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 5s "$bin" --no-write-log write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_log)"
  [[ ! -e $log ]] || fail "--no-write-log should prevent log file creation at $log"
}

function sftp_write_disabled_by_env_var { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  MADDER_WRITE_LOG=0 run_madder write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_log)"
  [[ ! -e $log ]] || fail "MADDER_WRITE_LOG=0 should prevent log file creation at $log"
}
