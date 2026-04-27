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

today_sftp_session_file() {
  local date
  date="$(date -u +%Y-%m-%d)"
  local day_dir="$XDG_LOG_HOME/madder/inventory_log/$date"

  if [[ ! -d $day_dir ]]; then
    return 1
  fi

  ls -1 "$day_dir"/*.hyphence 2>/dev/null | head -n 1
}

# session_body strips the 4-line hyphence header (---, ! type, ---,
# blank separator) from a session file, leaving just the NDJSON body.
sftp_session_body() {
  tail -n +5 "$1"
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
  log="$(today_sftp_session_file)" || fail "no session file under $XDG_LOG_HOME/madder/inventory_log/"
  [[ -s $log ]] || fail "expected non-empty session file at $log"

  local body
  body="$(sftp_session_body "$log")"

  local n
  n="$(echo "$body" | grep -c '"op":"written"' || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n. body:
$body"
}

function sftp_write_disabled_by_no_inventory_log_flag { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  # --no-inventory-log is a global flag; place it before the subcommand.
  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 5s "$bin" --no-inventory-log write .sftp-test "$blob"
  assert_success

  local day_dir
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$(date -u +%Y-%m-%d)"
  if [[ -d $day_dir ]]; then
    local count
    count="$(ls -1 "$day_dir"/*.hyphence 2>/dev/null | wc -l)"
    [[ $count -eq 0 ]] || fail "--no-inventory-log should prevent session file creation; found $count file(s) in $day_dir"
  fi
}

function sftp_write_disabled_by_env_var { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  MADDER_INVENTORY_LOG=0 run_madder write .sftp-test "$blob"
  assert_success

  local day_dir
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$(date -u +%Y-%m-%d)"
  if [[ -d $day_dir ]]; then
    local count
    count="$(ls -1 "$day_dir"/*.hyphence 2>/dev/null | wc -l)"
    [[ $count -eq 0 ]] || fail "MADDER_INVENTORY_LOG=0 should prevent session file creation; found $count file(s) in $day_dir"
  fi
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

function sftp_write_json_check_present { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'already here\n' >"$blob"
  run_madder write -format json .sftp-test "$blob"
  assert_success

  run_madder write -check -format json .sftp-test "$blob"
  assert_success
  assert_output --partial '"present":true'
}

function sftp_write_json_check_missing { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'not stored\n' >"$blob"

  run_madder write -check -format json .sftp-test "$blob"
  assert_failure
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

function sftp_init_compression_default { # @test
  # Mirrors init_compression_default for SFTP. The local test uses
  # assert_output (full-output equality) but the SFTP transport emits
  # dialing/connecting log lines that bats' merged run output captures
  # alongside the value, so anchor on assert_line for the printed
  # info-repo value instead. Per ADR 0005 / #60: compression-type is a
  # blob-store property and must come from the remote TomlV3.
  init_sftp_test_store

  run_madder info-repo .sftp-test compression-type
  assert_success
  assert_line 'zstd'
}

function sftp_init_hash_type_id_default { # @test
  # SFTP-init bootstrap writes a remote TomlV3 with HashTypeDefault
  # (blake2b256). Per ADR 0005 / #60, info-repo must surface that
  # (and not the stub sha256 the local TomlSFTPV0 returns).
  init_sftp_test_store

  run_madder info-repo .sftp-test hash_type-id
  assert_success
  assert_line 'blake2b256'
}

function sftp_init_hash_buckets_default { # @test
  # hash_buckets is not in the local SFTP transport config at all —
  # it's a blob-store property that lives only on the remote TomlV3.
  # Without #60's GetBlobStoreConfig() fall-through this would fail
  # with "unsupported info key".
  init_sftp_test_store

  run_madder info-repo .sftp-test hash_buckets
  assert_success
  assert_line '[2]'
}

function sftp_info_repo_host_stays_local { # @test
  # Reading transport keys (host, port, user, …) on an SFTP store must
  # not open the SSH/SFTP connection. Backend-property keys
  # (compression-type, hash_type-id) still do.
  init_sftp_test_store

  run_madder info-repo .sftp-test host
  assert_success
  assert_line '127.0.0.1'
  refute_output --partial 'dialing'
  refute_output --partial 'connected to'
}

function sftp_info_repo_config_immutable_encodes_remote { # @test
  # Per ADR 0005 §"info-repo … config-immutable wire shape", the
  # config-immutable pseudo-key on an SFTP store encodes
  # GetBlobStoreConfig() (the remote TomlV3) — not the local
  # TomlSFTPV0 transport. The refute_output lines are the load-bearing
  # pin: they catch an encoder that accidentally falls back to local.
  init_sftp_test_store

  run_madder info-repo .sftp-test config-immutable
  assert_success
  assert_output --partial 'compression-type'
  assert_output --partial 'zstd'
  assert_output --partial 'hash_type-id'
  assert_output --partial 'blake2b256'
  refute_output --partial 'private-key-path'
  refute_output --partial 'remote-path'
}

function sftp_write_record_has_contracted_fields { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_session_file)" || fail "no session file"
  local line
  line="$(sftp_session_body "$log" | head -n 1)"

  # Every field the design contracts is present. The description field
  # is optional (omitempty) and expected to be absent here since
  # --log-description is not passed.
  echo "$line" | grep -q '"type":"blob-write-published-v1"' || fail "record missing type discriminator: $line"
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
