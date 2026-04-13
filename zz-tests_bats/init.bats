setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=init

function init_default { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  init_store
}

function init_idempotent_fails { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  init_store
  run_madder init -encryption none -lock-internal-files=false .default
  assert_failure
}

function init_compression_default { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder info-repo compression-type
  assert_output 'zstd'
}

function init_without_encryption { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder info-repo encryption
  assert_output ''
}

function init_with_encryption { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  run_madder init -encryption generate .encrypted
  assert_success
  run_madder info-repo .encrypted encryption
  assert_success
  assert_output --regexp '.+'
}

function init_inventory_archive { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder init-inventory-archive -encryption none .archive
  assert_success
}

function init_inventory_archive_with_encryption { # @test
  skip "blocked on dewey golf/command bug (madder#2, purse-first#38)"
  set_xdg "$BATS_TEST_TMPDIR"
  init_store
  run_madder init-inventory-archive -encryption generate .archive
  assert_success
  run_madder info-repo .archive encryption
  assert_success
  assert_output --regexp '.+'
}
