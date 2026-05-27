setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=list

function list_shows_digest_for_migrated { # @test
  init_store
  run_madder list -format=tap
  assert_success
  # Migrated stores render `<id>@<format>-<blech32>:`. `partial` does
  # a literal substring match (not a regex), so the leading `.` is
  # safe.
  assert_output --partial '.default@blake2b256-'
}

function list_flags_unmigrated_with_footer { # @test
  init_store
  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  run_madder list -format=tap
  assert_success
  assert_output --partial '(unmigrated)'
  assert_output --partial 'madder config-pin_digest .default'
}

function list_no_footer_when_all_migrated { # @test
  init_store
  run_madder list -format=tap
  assert_success
  refute_output --partial 'config-pin_digest'
  refute_output --partial '(unmigrated)'
}

function list_ndjson_includes_digest { # @test
  init_store
  run_madder list -format=ndjson
  assert_success
  assert_output --partial '"digest":"madder-blob_store-config-digest-v1@blake2b256-'
}

function list_ndjson_flags_legacy { # @test
  init_store
  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  run_madder list -format=ndjson
  assert_success
  assert_output --partial '"digest_missing":true'
}
