setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=contract,write,read,has

# compute_unstored_hash writes content to $1, computes its digest with
# `write -check` (which hashes without storing), and echoes the resulting
# blech32-valid blake2b256-... string. The caller's blob store must be
# initialized but should not already contain this content.
compute_unstored_hash() {
  local blob="$1"
  run_madder write -check -format json "$blob"
  echo "$output" | jq -r 'select(.id) | .id' | head -n1
}

function write_prints_digest_to_stdout { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "contract test content" >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  assert_line --regexp '^ok 1 - blake2b256-[A-Za-z0-9]+ '
}

function cat_exits_nonzero_on_missing_blob { # @test

  init_store

  local unstored="$BATS_TEST_TMPDIR/unstored.txt"
  echo "never-written-for-cat-test" >"$unstored"
  local missing_hash
  missing_hash="$(compute_unstored_hash "$unstored")"
  [[ -n $missing_hash ]] || fail "could not compute unstored hash"

  run_madder cat "$missing_hash"
  assert_failure
}

function has_exits_zero_for_existing_blob { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "has test content" >"$blob"
  local hash
  hash="$(write_blob_id "$blob")"

  run_madder has "$hash"
  assert_success
  assert_output --partial "found"
}

function has_exits_nonzero_for_missing_blob { # @test

  init_store

  local unstored="$BATS_TEST_TMPDIR/unstored.txt"
  echo "never-written-for-has-test" >"$unstored"
  local missing_hash
  missing_hash="$(compute_unstored_hash "$unstored")"
  [[ -n $missing_hash ]] || fail "could not compute unstored hash"

  run_madder has "$missing_hash"
  assert_failure
  assert_output --partial "not found"
}

function has_mixed_found_and_missing { # @test

  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "mixed test content" >"$blob"
  local hash
  hash="$(write_blob_id "$blob")"

  local unstored="$BATS_TEST_TMPDIR/unstored.txt"
  echo "never-written-for-has-mixed-test" >"$unstored"
  local missing_hash
  missing_hash="$(compute_unstored_hash "$unstored")"
  [[ -n $missing_hash ]] || fail "could not compute unstored hash"

  run_madder has "$hash" "$missing_hash"
  assert_failure
  assert_output --partial "found"
  assert_output --partial "not found"
}

function has_scopes_to_explicit_blob_store_id { # @test

  # Regression for #25: scoped `has .default <hash>` must not fall back to
  # the all-stores search.
  init_store
  run_madder init -encryption none .elsewhere
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "only in elsewhere" >"$blob"
  local hash
  hash="$(write_blob_id .elsewhere "$blob")"

  run_madder has "$hash"
  assert_success
  assert_output --partial "found"

  run_madder has .default "$hash"
  assert_failure
  assert_output --partial "not found"

  run_madder has .elsewhere "$hash"
  assert_success
  assert_output --partial $'\tfound'
}
