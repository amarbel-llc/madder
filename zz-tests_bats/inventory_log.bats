setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
}

# bats file_tags=inventory_log

# today_session_file finds the (single) hyphence session file written by
# the most recent madder invocation under today's date directory. Each
# CLI invocation owns its own session file, so callers running exactly
# one madder write per test should find exactly one file. Callers
# running N writes get N session files.
today_session_file() {
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
session_body() {
  tail -n +5 "$1"
}

function inventory_log_emits_written_record { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"

  run_madder write "$blob"
  assert_success

  local log
  log="$(today_session_file)" || fail "no session file under $XDG_LOG_HOME/madder/inventory_log/"
  [[ -s $log ]] || fail "expected non-empty session file at $log"

  local body
  body="$(session_body "$log")"

  # Exactly one NDJSON record, op=written, with the new "type" field.
  local n
  n="$(echo "$body" | grep -c '"op":"written"' || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n. body:$'\n'$body"

  echo "$body" | grep -q '"type":"blob-write-published-v1"' ||
    fail "record missing type discriminator: $body"
}

function inventory_log_duplicate_is_exists { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "same bytes" >"$blob"

  # First write: op=written. Second write of identical bytes: op=exists
  # (link(2) returns EEXIST, verify-on-collision is not enabled by
  # default). Each invocation produces its own session file.
  run_madder write "$blob"
  assert_success
  run_madder write "$blob"
  assert_success

  local date day_dir
  date="$(date -u +%Y-%m-%d)"
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$date"

  # Combine all today's session bodies for cross-session counting.
  local combined
  combined="$(for f in "$day_dir"/*.hyphence; do session_body "$f"; done)"

  local written_count exists_count
  written_count="$(echo "$combined" | grep -c '"op":"written"' || true)"
  exists_count="$(echo "$combined" | grep -c '"op":"exists"' || true)"

  [[ $written_count -eq 1 ]] || fail "expected 1 written, got $written_count"
  [[ $exists_count -eq 1 ]] || fail "expected 1 exists, got $exists_count"
}

function inventory_log_disabled_by_no_inventory_log_flag { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 2s "$bin" --no-inventory-log write "$blob"
  assert_success

  local day_dir
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$(date -u +%Y-%m-%d)"
  if [[ -d $day_dir ]]; then
    local count
    count="$(ls -1 "$day_dir"/*.hyphence 2>/dev/null | wc -l)"
    [[ $count -eq 0 ]] || fail "--no-inventory-log should prevent session file creation; found $count file(s) in $day_dir"
  fi
}

function inventory_log_disabled_by_env_var { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  MADDER_INVENTORY_LOG=0 run_madder write "$blob"
  assert_success

  local day_dir
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$(date -u +%Y-%m-%d)"
  if [[ -d $day_dir ]]; then
    local count
    count="$(ls -1 "$day_dir"/*.hyphence 2>/dev/null | wc -l)"
    [[ $count -eq 0 ]] || fail "MADDER_INVENTORY_LOG=0 should prevent session file creation; found $count file(s) in $day_dir"
  fi
}

function inventory_log_record_has_contracted_fields { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write "$blob"
  assert_success

  local log
  log="$(today_session_file)" || fail "no session file"

  local line
  line="$(session_body "$log" | head -n 1)"

  # Every field the design contracts is present. The description field
  # is optional (omitempty) and expected to be absent when
  # --log-description is not passed — covered by a separate test below.
  echo "$line" | grep -q '"type":"blob-write-published-v1"' || fail "record missing type discriminator: $line"
  echo "$line" | grep -q '"ts":' || fail "record missing ts field: $line"
  echo "$line" | grep -q '"utility":"madder"' || fail "record utility != madder: $line"
  echo "$line" | grep -q '"pid":' || fail "record missing pid field: $line"
  echo "$line" | grep -q '"store_id":' || fail "record missing store_id: $line"
  echo "$line" | grep -q '"markl_id":' || fail "record missing markl_id: $line"
  echo "$line" | grep -q '"size":' || fail "record missing size field: $line"
  echo "$line" | grep -q '"op":"written"' || fail "record op != written: $line"

  # description is omitempty when the flag is absent.
  echo "$line" | grep -q '"description"' &&
    fail "description field should be absent when --log-description not passed: $line" ||
    true
}

function inventory_log_description_flag_stamps_records { # @test
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write --log-description 'imported Q3 backup tapes' "$blob"
  assert_success

  local log
  log="$(today_session_file)" || fail "no session file"
  [[ -s $log ]] || fail "expected non-empty session file at $log"

  local line
  line="$(session_body "$log" | head -n 1)"
  echo "$line" | grep -q '"description":"imported Q3 backup tapes"' ||
    fail "record missing or wrong description: $line"
}

function inventory_log_empty_description_omits_field { # @test
  # Passing --log-description with an empty string should still omit the
  # field (omitempty); otherwise users who set it to an empty default
  # get noisy "description":"" in every record.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  run_madder write --log-description '' "$blob"
  assert_success

  local log
  log="$(today_session_file)" || fail "no session file"

  local line
  line="$(session_body "$log" | head -n 1)"
  echo "$line" | grep -q '"description"' &&
    fail "empty --log-description should omit the field: $line" ||
    true
}

function inventory_log_session_file_has_hyphence_header { # @test
  # New: the session file must start with a hyphence document whose
  # metadata is exactly `! madder-inventory_log-ndjson-v1`. This pins
  # the on-disk envelope contract from the design doc.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello" >"$blob"

  run_madder write "$blob"
  assert_success

  local log
  log="$(today_session_file)" || fail "no session file"

  local first_line
  first_line="$(head -n 1 "$log")"
  [[ "$first_line" == "---" ]] || fail "session file must start with --- (got $first_line)"

  local type_line
  type_line="$(sed -n '2p' "$log")"
  [[ "$type_line" == "! madder-inventory_log-ndjson-v1" ]] ||
    fail "session file metadata type must be madder-inventory_log-ndjson-v1, got: $type_line"

  local close_line
  close_line="$(sed -n '3p' "$log")"
  [[ "$close_line" == "---" ]] || fail "session file must close metadata with --- on line 3 (got: $close_line)"

  local separator
  separator="$(sed -n '4p' "$log")"
  [[ -z "$separator" ]] || fail "line 4 must be the empty separator (got: $separator)"
}
