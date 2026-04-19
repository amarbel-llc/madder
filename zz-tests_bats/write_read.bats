setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=write,read

function write_and_cat { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write returned empty hash"

  run_madder cat "$hash"
  assert_success
  assert_output --partial "hello world"
}

function write_from_stdin { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "stdin content" >"$blob"

  run_madder write -format tap -
  assert_success
}

function list_after_write { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "list test" >"$blob"

  run_madder write -format tap "$blob"
  assert_success

  run_madder list
  assert_success
  assert_output
}

function write_warns_when_file_shadows_store { # @test

  # Init two stores: the default .default (CWD-scoped) plus one named
  # "shadowed" (XDG user). Then create a file with the same bare name in
  # the CWD. A bare `write shadowed` should resolve to the file but emit
  # a warning comment pointing at the store collision.
  init_store
  run_madder init -encryption none -lock-internal-files=false shadowed
  assert_success

  echo "file content" >shadowed

  run_madder write -format tap shadowed
  assert_success
  assert_output --partial "shadows blob store"
  assert_output --partial "'./shadowed'"
  assert_output --partial '".shadowed"'
}

function write_no_warning_when_no_store_collision { # @test

  # Control: same file pattern, but no store with matching name. No
  # warning should fire.
  init_store

  echo "file content" >unique_filename

  run_madder write -format tap unique_filename
  assert_success
  refute_output --partial "shadows blob store"
}

# NDJSON output mode (issue #26) -----------------------------------------

function write_json_emits_ndjson_record { # @test

  # Explicit -format=json returns one NDJSON object per blob with
  # id/size/source fields.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'hello world\n' >"$blob"

  run_madder write -format json "$blob"
  assert_success
  assert_output --regexp '^\{"id":"[^"]+","size":12,"source":"[^"]+blob\.txt"\}$'
}

function write_auto_detects_json_when_stdout_is_pipe { # @test

  # Default -format=auto should emit NDJSON when stdout is not a TTY,
  # which is always the case under bats `run`.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'auto detect\n' >"$blob"

  run_madder write "$blob"
  assert_success
  assert_output --regexp '^\{"id":"[^"]+","size":12,"source":"[^"]+blob\.txt"\}$'
}

function write_json_check_present { # @test

  # -check with a blob already in the store emits present:true.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'already here\n' >"$blob"
  run_madder write -format json "$blob"
  assert_success

  run_madder write -check -format json "$blob"
  assert_success
  assert_output --partial '"present":true'
}

function write_json_check_missing { # @test

  # -check with a blob NOT in the store emits present:false and the
  # command exits non-zero.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'not stored\n' >"$blob"

  run_madder write -check -format json "$blob"
  assert_failure
  assert_output --partial '"present":false'
}

function write_tap_override_on_pipe { # @test

  # Explicit -format=tap forces TAP even when stdout is piped.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'tap override\n' >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  assert_output --partial 'TAP version 14'
  assert_output --partial 'ok 1 - '
  refute_output --partial '"id":'
}

function write_json_rejects_unknown_format { # @test

  init_store

  run_madder write -format bogus -
  assert_failure
  assert_output --partial 'unsupported output format'
}

function write_json_warning_goes_to_stderr { # @test

  # Shadow warnings are informational; in JSON mode they must route to
  # stderr so the stdout stream stays valid NDJSON. bats run merges
  # stderr into $output, so we can't separate streams here — but we can
  # at least assert the warning does NOT appear mid-NDJSON line.
  init_store
  run_madder init -encryption none -lock-internal-files=false shadowed
  assert_success

  echo "file content" >shadowed

  run_madder write -format json shadowed
  assert_success
  # Exactly one NDJSON record on stdout (merged with stderr by run).
  local n
  n="$(echo "$output" | grep -c '^{')"
  [[ $n -eq 1 ]] || fail "expected 1 NDJSON record, got $n"
  assert_output --partial 'shadows blob store'
}
