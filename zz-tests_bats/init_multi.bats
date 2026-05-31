setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=init_multi

# Flags must precede the positional blob-store-id: dewey's flag parser
# (Go-stdlib semantics) stops at the first non-flag token, so the
# store-id always comes last (mirrors `init -encryption none .default`).

# Scenario 1: mirror across two local leaves.
function init_multi_mirror { # @test
  init_store
  run_madder init -encryption none .ssd
  assert_success
  run_madder init -encryption none .nvme
  assert_success

  run_madder init-multi --mode mirror \
    --mirror-store .ssd --mirror-store .nvme .fanout
  assert_success

  local config=".madder/local/share/blob_stores/fanout/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"
  run grep -E 'mode = "mirror"' "$config"
  assert_success
  # references are digest-bearing on disk
  run grep -E 'blake2b256-' "$config"
  assert_success

  # the multi composes transparently through list
  run_madder list
  assert_success
}

# Scenario 2: write_through WITH read_fill.
function init_multi_write_through_read_fill { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success

  run_madder init-multi --mode write_through \
    --write-store .default --read-store .archive --read-fill .cache
  assert_success

  local config=".madder/local/share/blob_stores/cache/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"
  run grep -E 'read-fill = true' "$config"
  assert_success
}

# Scenario 3: write_through WITHOUT read_fill.
function init_multi_write_through_no_read_fill { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success

  run_madder init-multi --mode write_through \
    --write-store .default --read-store .archive --no-read-fill .cache
  assert_success

  local config=".madder/local/share/blob_stores/cache/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"
  run grep -E 'read-fill = false' "$config"
  assert_success
}

# Scenario 4: nested multi-of-multi.
function init_multi_nested { # @test
  init_store
  run_madder init -encryption none .ssd
  assert_success
  run_madder init -encryption none .nvme
  assert_success
  run_madder init -encryption none .tape
  assert_success

  run_madder init-multi --mode mirror \
    --mirror-store .ssd --mirror-store .nvme .fast
  assert_success
  run_madder init-multi --mode write_through \
    --write-store .fast --read-store .tape --read-fill .tiered
  assert_success

  # the whole graph resolves at load time
  run_madder list
  assert_success
}
