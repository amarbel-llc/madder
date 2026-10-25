setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=info_repo

# The tests tagged `rust_cli` also run against the Rust crate's madder-grammar
# CLI (crates/madder-store) as MADDER_BIN, via `just test-bats-rust-cli`.

# bats test_tags=rust_cli
function compression_type { # @test
  init_store
  run_madder info-repo compression-type
  assert_success
  assert_output 'zstd'
}

# bats test_tags=rust_cli
function encryption_none { # @test
  init_store
  run_madder info-repo encryption
  assert_success
  assert_output ''
}

# bats test_tags=rust_cli
function unknown_key_fails { # @test
  init_store
  run_madder info-repo nonexistent-key
  assert_failure
}

# bats test_tags=rust_cli
function single_arg_resolves_as_store_id { # @test
  init_store
  run_madder info-repo .default
  assert_success
  # Multi-line hyphence config; pin one well-known key to prove the
  # full config (not a single key value) was emitted.
  assert_output --partial 'compression-type'
}
