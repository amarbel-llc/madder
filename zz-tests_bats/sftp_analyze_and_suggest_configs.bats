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

# --- helpers used by the tests below -------------------------------------

# Snapshot every regular file under $1 as one line per file:
# "<relative-path> <size> <mtime-seconds>". Sorted for stable diff.
snapshot_tree() {
  ( cd "$1" && find . -type f -printf '%P %s %T@\n' | sort )
}

# --- bad inputs / boundaries (#11, #12) ----------------------------------

function bad_input_missing_ssh_host { # @test
  run_madder sftp-analyze-and-suggest-configs -remote-path /tmp
  assert_failure
  assert_output --partial '-ssh-host'
}

function bad_input_missing_remote_path { # @test
  run_madder sftp-analyze-and-suggest-configs -ssh-host madder-test
  assert_failure
  assert_output --partial '-remote-path'
}

function bad_input_key_path_does_not_exist { # @test
  run_madder sftp-analyze-and-suggest-configs \
    -ssh-host madder-test \
    -remote-path /tmp \
    -key /no/such/key/file
  assert_failure
  assert_output --partial 'no such file or directory'
}

function empty_remote_path_errors_clearly { # @test
  local remote_root="$BATS_TEST_TMPDIR/empty-store"
  mkdir -p "$remote_root"

  TMPDIR="$BATS_TEST_TMPDIR/suggest" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host "testuser@127.0.0.1:$SFTP_PORT" \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS"
  assert_failure
  assert_output --partial 'cannot discover bucket structure'
}

# --- read-only invariant (#7) --------------------------------------------

function probing_phase_is_read_only { # @test
  local remote_root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$remote_root"
  for i in 1 2 3 4 5; do
    place_legacy_blob_at_correct_path "$remote_root" zstd none - "blob $i"
  done

  local before="$BATS_TEST_TMPDIR/snap-before"
  snapshot_tree "$remote_root" >"$before"

  TMPDIR="$BATS_TEST_TMPDIR/suggest" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host "testuser@127.0.0.1:$SFTP_PORT" \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    -limit 3

  local after="$BATS_TEST_TMPDIR/snap-after"
  snapshot_tree "$remote_root" >"$after"

  diff "$before" "$after" || \
    fail "remote tree changed during default (no -yes-to-all) probe"
}

# --- -limit is honored (#9) ----------------------------------------------

function limit_caps_sample_count { # @test
  local remote_root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$remote_root"
  # Twenty blobs is plenty more than -limit 3 needs but cheap enough.
  for i in $(seq 1 20); do
    place_legacy_blob_at_correct_path "$remote_root" zstd none - "blob $i payload"
  done

  TMPDIR="$BATS_TEST_TMPDIR/suggest" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host "testuser@127.0.0.1:$SFTP_PORT" \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    -limit 3
  assert_success
  # TAP "verified=K/N" reports samples drawn as N. -limit 3 → all
  # candidates show /3.
  assert_output --partial 'verified=3/3'
  refute_output --partial 'verified=10/10'
}

# --- legacy layout detection (#2-#4) -------------------------------------

# Common shape: hand-craft a single-hash bucketed tree with no remote
# blob_store-config, then assert the matching candidate verifies all
# samples and the others fail at the decompress stage.
analyze_legacy_layout() {
  local comp="$1"
  local remote_root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$remote_root"
  for i in 1 2 3 4 5; do
    place_legacy_blob_at_correct_path \
      "$remote_root" "$comp" none - "blob $i some payload bytes"
  done

  TMPDIR="$BATS_TEST_TMPDIR/suggest" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host "testuser@127.0.0.1:$SFTP_PORT" \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    -limit 5
  assert_success
  # The matching candidate verifies all 5 samples cleanly.
  assert_output --partial "ok 1 - $comp/none verified=5/5"
}

function legacy_detection_none { # @test
  analyze_legacy_layout none
}

function legacy_detection_zstd { # @test
  analyze_legacy_layout zstd
}

function legacy_detection_gzip { # @test
  analyze_legacy_layout gzip
}

# --- bootstrap end-to-end with -yes-to-all (#8) --------------------------

function bootstrap_yes_to_all_writes_remote_config { # @test
  local remote_root="$BATS_TEST_TMPDIR/legacy-store"
  mkdir -p "$remote_root"
  for i in 1 2 3; do
    place_legacy_blob_at_correct_path "$remote_root" none none - "blob $i"
  done

  # No remote config exists; -yes-to-all should auto-confirm bootstrap
  # and write one via blob_stores.WriteRemoteConfig.
  [[ ! -e "$remote_root/blob_store-config" ]] || \
    fail "test fixture pre-condition: blob_store-config must not pre-exist"

  TMPDIR="$BATS_TEST_TMPDIR/suggest" run_madder sftp-analyze-and-suggest-configs \
    -ssh-host "testuser@127.0.0.1:$SFTP_PORT" \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    -limit 3 \
    -yes-to-all
  assert_success

  [[ -e "$remote_root/blob_store-config" ]] || \
    fail "blob_store-config not written to remote"

  local mode
  mode="$(stat -c '%a' "$remote_root/blob_store-config")"
  [[ $mode == 444 ]] || fail "expected mode 444; got $mode"

  # Config should have single_hash = true (legacy single-hash layout)
  # so subsequent reads walk the bucket tree correctly per #149.
  grep -q 'single_hash = true' "$remote_root/blob_store-config" || \
    fail "blob_store-config missing 'single_hash = true' for legacy layout: $(cat "$remote_root/blob_store-config")"
}
