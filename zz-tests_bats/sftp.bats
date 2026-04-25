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

function sftp_write_emits_written_record { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"

  # The remote layout is just a regular filesystem path (the test
  # SFTP server has no notion of a chroot). Seed it with a valid
  # blob_store-config so madder's first write doesn't bail trying
  # to discover one.
  local remote_root="$BATS_TEST_TMPDIR/sftp-remote"
  mkdir -p "$remote_root"
  cat >"$remote_root/blob_store-config" <<'EOF'
---
! toml-blob_store_config-v3
---

hash_buckets = [2]
hash_type-id = "blake2b256"
encryption = []
compression-type = "zstd"
EOF

  run_madder init-sftp-explicit \
    -host 127.0.0.1 \
    -port "$SFTP_PORT" \
    -user testuser \
    -password anything \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    .sftp-test
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  # Per `madder write`: a leading blob-store-id arg switches the active
  # store for subsequent file args.
  run_madder write .sftp-test "$blob"
  assert_success

  local date
  date="$(date -u +%Y-%m-%d)"
  local log="$XDG_LOG_HOME/madder/blob-writes-$date.ndjson"
  [[ -s $log ]] || fail "expected write-log at $log, got none"

  local n
  n="$(grep -c '"op":"written"' "$log" || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n. log:
$(cat "$log")"
}
