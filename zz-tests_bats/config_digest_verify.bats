setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=config_digest

function decode_detects_tampered_config { # @test
  # FDR-0008 Phase 1: any madder command that reads a blob_store-config
  # must refuse a tampered one. The body-bytes digest was computed at
  # init time over the canonical body; changing a semantic field
  # (here: hash_buckets) invalidates it.
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"

  # The config is 0444. Re-chmod, mutate, re-chmod back.
  chmod 0644 "$config"
  sed -i 's/hash_buckets = \[2\]/hash_buckets = [3]/' "$config"
  chmod 0444 "$config"

  run_madder list
  assert_failure
  assert_output --partial 'expected digest'
  assert_output --partial 'but got'
}
