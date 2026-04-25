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

function sftp_write_and_cat { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"

  run_madder write -format tap .sftp-test "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write returned empty hash. output: $output"

  run_madder cat .sftp-test "$hash"
  assert_success
  assert_output --partial "hello world"
}

function sftp_write_from_stdin { # @test
  init_sftp_test_store

  run_madder write -format tap .sftp-test -
  assert_success
}

function sftp_list_after_write { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "list test" >"$blob"

  run_madder write -format tap .sftp-test "$blob"
  assert_success

  run_madder list
  assert_success
  assert_output
}

function sftp_write_warns_when_file_shadows_store { # @test
  # Init an SFTP store named `shadowed`, then create a file with the
  # same bare name in CWD. Bare `write shadowed` should resolve to the
  # file but warn about the blob-store-id collision — same semantics
  # as the local-store version, since shadow detection is name-based
  # and backend-agnostic.
  init_sftp_test_store "$BATS_TEST_TMPDIR/sftp-shadowed" shadowed

  echo "file content" >shadowed

  run_madder write -format tap shadowed
  assert_success
  assert_output --partial "shadows blob-store-id"
  assert_output --partial "'./shadowed'"
  # Unlike `init -encryption none shadowed`, which yields a CWD-scoped
  # `.shadowed` store id in the warning, init-sftp-explicit registers
  # the id at user (XDG) scope where the bare name is the canonical
  # form — the warning quotes it as "shadowed".
  assert_output --partial '"shadowed"'
}

function sftp_write_no_warning_when_no_store_collision { # @test
  # Control: SFTP store id does not collide with any file in CWD;
  # write of an unrelated file must not surface a shadow warning.
  init_sftp_test_store

  echo "file content" >unique_filename

  run_madder write -format tap unique_filename
  assert_success
  refute_output --partial "shadows blob store"
}

function sftp_write_json_emits_ndjson_record { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'hello world\n' >"$blob"

  run_madder write -format json .sftp-test "$blob"
  assert_success
  # Non-default store, so the record carries a "store" field in
  # addition to id/size/source. Field order is a commitment of the
  # write contract. Use assert_line --regexp because $output
  # interleaves the SFTP transport's dialing/connecting log lines
  # with the NDJSON record; the local write_json_emits_ndjson_record
  # version has clean output and so can anchor at $output level.
  assert_line --regexp '^\{"id":"[^"]+","size":12,"source":"[^"]+blob\.txt","store":"\.sftp-test"\}$'
}

# sftp_write_json_check_present is intentionally absent: blocked by
# madder#59 — write -check against SFTP recomputes the file digest
# using the global default hash (sha256) before the lazy SFTP init
# has read the remote config (blake2b256). Re-add once #59 is fixed.

function sftp_write_json_check_missing { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'not stored\n' >"$blob"

  run_madder write -check -format json .sftp-test "$blob"
  assert_failure
  # CAVEAT: per madder#59, -check against SFTP currently always
  # returns present:false because of a lazy-init bug. This test
  # therefore passes for the right reason AND the wrong reason;
  # tighten or re-evaluate once #59 lands.
  assert_output --partial '"present":false'
}

function sftp_write_json_warning_goes_to_stderr { # @test
  # Shadow warnings are informational; in JSON mode they must route
  # to stderr so stdout stays valid NDJSON. bats `run` merges streams
  # but the count of NDJSON records on the merged output should still
  # be exactly one — proves the warning didn't pollute stdout.
  init_sftp_test_store "$BATS_TEST_TMPDIR/sftp-shadowed" shadowed

  echo "file content" >shadowed

  run_madder write -format json shadowed
  assert_success
  local n
  n="$(echo "$output" | grep -c '^{' || true)"
  [[ $n -eq 1 ]] || fail "expected 1 NDJSON record, got $n. output:
$output"
  assert_output --partial 'shadows blob-store-id'
}

function sftp_init_idempotent_fails { # @test
  init_sftp_test_store

  # A second init for the same id should fail because the local
  # config file already exists; this matches the local-store
  # init_idempotent_fails contract.
  run_madder init-sftp-explicit \
    -host 127.0.0.1 \
    -port "$SFTP_PORT" \
    -user testuser \
    -password anything \
    -remote-path "$BATS_TEST_TMPDIR/sftp-remote" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    .sftp-test
  assert_failure
}

function sftp_init_probe_compression_default { # @test
  # Diagnostic: the local init_compression_default asserts that
  # `info-repo compression-type` returns "zstd". For SFTP, info-repo
  # reads the LOCAL config (TomlSFTPV0) which carries only transport
  # fields — compression-type lives in the remote blob_store-config
  # and is invisible to info-repo today. Tracked as madder#60.
  #
  # When #60 lands, flip the assertions to mirror the local
  # init_compression_default (assert_success + assert_output 'zstd')
  # and rename to sftp_init_compression_default.
  init_sftp_test_store

  run_madder info-repo .sftp-test compression-type
  assert_failure
  assert_output --partial 'unsupported info key'
}

function sftp_write_record_has_contracted_fields { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_log)"
  local line
  line="$(head -n 1 "$log")"

  # Every field the ADR contracts is present. The description field
  # is optional (omitempty) and expected to be absent here since
  # --log-description is not passed.
  echo "$line" | grep -q '"ts":' || fail "record missing ts field: $line"
  echo "$line" | grep -q '"utility":"madder"' || fail "record utility != madder: $line"
  echo "$line" | grep -q '"pid":' || fail "record missing pid field: $line"
  echo "$line" | grep -q '"store_id":' || fail "record missing store_id: $line"
  echo "$line" | grep -q '"markl_id":' || fail "record missing markl_id: $line"
  echo "$line" | grep -q '"size":' || fail "record missing size field: $line"
  echo "$line" | grep -q '"op":"written"' || fail "record op != written: $line"

  echo "$line" | grep -q '"description"' &&
    fail "description field should be absent when --log-description not passed: $line" ||
    true
}
