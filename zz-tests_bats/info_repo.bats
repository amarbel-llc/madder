setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=info_repo

function compression_type { # @test

  init_store
  run_madder info-repo compression-type
  assert_success
  assert_output 'zstd'
}

function encryption_none { # @test

  init_store
  run_madder info-repo encryption
  assert_success
  assert_output ''
}

function unknown_key_fails { # @test

  init_store
  run_madder info-repo nonexistent-key
  assert_failure
}

function single_arg_resolves_as_store_id { # @test

  init_store
  run_madder info-repo .default
  assert_success
  # The immutable config is multi-line hyphence; pinning one
  # well-known key proves the immutable config (not a single key
  # value) was emitted.
  assert_output --partial 'compression-type'
}
