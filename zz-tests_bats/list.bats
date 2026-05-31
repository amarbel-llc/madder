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

function list_tree_renders_multi_graph { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success
  run_madder init-multi --mode write_through \
    --write-store .default --read-store .archive --read-fill .cache
  assert_success

  # -tree only affects text output; force text mode so the bats-piped
  # stdout (which would otherwise default to ndjson) exercises the tree
  # renderer rather than the structured emitter.
  run_madder list -tree -format=tap
  assert_success
  assert_output --partial 'multi'
  assert_output --partial 'write_through'
  # the tree shows the referenced leaves under the multi
  assert_output --partial '.archive'
  # the referenced leaf is indented beneath the multi with its role
  assert_output --partial '(read)'
}

function list_ndjson_multi_fields { # @test
  init_store
  run_madder init -encryption none .archive
  assert_success
  run_madder init-multi --mode write_through \
    --write-store .default --read-store .archive --read-fill .cache
  assert_success

  run_madder list -format=ndjson
  assert_success
  assert_output --partial '"mode":"write_through"'
  assert_output --partial '"read_fill":true'
}
