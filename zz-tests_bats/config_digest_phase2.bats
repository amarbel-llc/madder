setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=config_digest_phase2

# A digest-bearing id matching the on-disk config resolves cleanly.
function phase2_matching_digest_resolves { # @test
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  local digest
  digest="$(grep -E '^@ blake2b256-' "$config" | awk '{print $2}')"
  [[ -n $digest ]] || fail "no @ digest line in $config"

  run_madder fsck ".default@$digest"
  assert_success
}

# A digest that doesn't match the on-disk config fails with
# markl.ErrNotEqual ("expected digest ... but got ...").
function phase2_mismatched_digest_refuses { # @test
  init_store

  # Syntactically valid blake2b256 digest borrowed from the
  # commands_mcp test fixture; intentionally won't match anything.
  local bogus="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

  run_madder fsck ".default@$bogus"
  assert_failure
  assert_output --partial 'expected digest'
  assert_output --partial 'but got'
}

# An id with a digest against a legacy (un-migrated) config fails
# with the dedicated typed error pointing at config-pin_digest.
function phase2_digest_against_legacy_refuses { # @test
  init_store

  # Strip the @ line to simulate a pre-FDR-0008 config.
  local config=".madder/local/share/blob_stores/default/blob_store-config"
  chmod 0644 "$config"
  sed -i.bak '/^@ /d' "$config" && rm "$config.bak"
  chmod 0444 "$config"

  local bogus="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

  run_madder fsck ".default@$bogus"
  assert_failure
  assert_output --partial 'unmigrated'
  assert_output --partial 'config-pin_digest'
}
