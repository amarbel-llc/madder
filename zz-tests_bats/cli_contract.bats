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

function has_default_output_includes_store_id { # @test

  # #171: a single-store hit emits `<digest>\tfound\t<store-id>` (three
  # tab-separated columns).
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "store-id column test" >"$blob"
  local hash
  hash="$(write_blob_id "$blob")"

  run_madder has "$hash"
  assert_success
  assert_line --regexp $'^'"$hash"$'\tfound\t.+$'
}

function has_all_emits_one_line_per_store { # @test

  # #171: with -all, every store that holds the blob produces its own
  # `<digest>\tfound\t<store-id>` line.
  init_store
  run_madder init -encryption none .elsewhere
  assert_success

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "multi-store has-all test" >"$blob"
  local hash
  hash="$(write_blob_id "$blob")"
  # Write the same content into the second store so both should report
  # the blob as present.
  hash2="$(write_blob_id .elsewhere "$blob")"
  [[ $hash == "$hash2" ]] || fail "expected identical digest across stores"

  run_madder has -all "$hash"
  assert_success
  # Two `found` lines, one per store.
  local found_lines
  found_lines="$(printf '%s\n' "$output" | grep -c $'\tfound\t' || true)"
  [[ $found_lines -eq 2 ]] || fail "expected 2 found lines, got $found_lines: $output"
}

function has_all_with_explicit_store_errors { # @test

  # #171: -all combined with an explicit blob-store-id arg is a usage
  # error and exits nonzero.
  init_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "all-plus-scope test" >"$blob"
  local hash
  hash="$(write_blob_id "$blob")"

  run_madder has -all .default "$hash"
  assert_failure
  assert_output --partial "-all"
}
