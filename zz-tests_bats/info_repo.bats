setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=info_repo

function compression_type { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder info-repo compression-type
  assert_success
  assert_output 'zstd'
}

function encryption_none { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder info-repo encryption
  assert_success
  assert_output ''
}

function unknown_key_fails { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder info-repo nonexistent-key
  assert_failure
}
