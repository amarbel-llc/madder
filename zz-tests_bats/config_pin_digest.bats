setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=config_pin_digest

function config_pin_digest_mints_on_legacy { # @test
  # FDR-0008 Phase 1 migration: a pre-FDR-0008 config (no @ line) is
  # migrated to carry an @ digest line.
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  run grep -E '^@ blake2b256-' "$config"
  assert_failure  # confirm legacy shape

  run_madder config-pin_digest .default
  assert_success

  run grep -E '^@ blake2b256-' "$config"
  assert_success
}

function config_pin_digest_idempotent { # @test
  init_store

  run_madder config-pin_digest .default
  assert_success

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  local before
  before=$(cat "$config")

  run_madder config-pin_digest .default
  assert_success

  local after
  after=$(cat "$config")
  assert_equal "$before" "$after"
}

function config_pin_digest_all { # @test
  init_store
  run_madder init-inventory-archive -encryption none .archive
  assert_success

  for config in \
    .madder/local/share/blob_stores/default/blob_store-config \
    .madder/local/share/blob_stores/archive/blob_store-config
  do
    chmod 0644 "$config"
    sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
    chmod 0444 "$config"
  done

  run_madder config-pin_digest -all
  assert_success

  for config in \
    .madder/local/share/blob_stores/default/blob_store-config \
    .madder/local/share/blob_stores/archive/blob_store-config
  do
    run grep -E '^@ blake2b256-' "$config"
    assert_success
  done
}

function config_pin_digest_rejects_no_target { # @test
  init_store
  run_madder config-pin_digest
  assert_failure
  assert_output --partial 'specify --all or one or more blob-store-ids'
}

function config_pin_digest_rejects_both_modes { # @test
  init_store
  run_madder config-pin_digest -all .default
  assert_failure
  assert_output --partial '--all and explicit ids are mutually exclusive'
}
