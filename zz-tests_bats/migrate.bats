setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=migrate

# init-from --from-store is the FDR-0010 copy-migration: a new store with a
# freshly minted uuid instance identity (and thus a distinct config digest),
# optionally populated from the source store's blobs, with the source left
# byte-untouched.

function init_from_from_store_mints_new_instance_and_copies_blobs { # @test
  init_store .source

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "migrate round trip" >"$blob"
  local hash
  hash="$(write_blob_id .source "$blob")"
  [[ -n $hash ]] || fail "write to source returned no blob id"

  local src_config=".madder/local/share/blob_stores/source/blob_store-config"
  [[ -f $src_config ]] || fail "source config missing: $src_config"
  local src_before
  src_before="$(cat "$src_config")"

  run_madder init-from --from-store .source --sync .dest
  assert_success

  local dst_config=".madder/local/share/blob_stores/dest/blob_store-config"
  [[ -f $dst_config ]] || fail "dest config missing: $dst_config"

  # The new store carries a uuidv7 instance-id...
  run cat "$dst_config"
  assert_success
  assert_output --partial 'instance-id = "uuidv7-'

  # ...that DIFFERS from the source's (a distinct instance)...
  local src_iid dst_iid
  src_iid="$(grep instance-id "$src_config")"
  dst_iid="$(grep instance-id "$dst_config")"
  [[ -n $dst_iid ]] || fail "dest has no instance-id"
  [[ $src_iid != "$dst_iid" ]] || fail "dest instance-id must differ from source: $dst_iid"

  # ...and a distinct config digest (the @ line inherits the id's entropy).
  local src_digest dst_digest
  src_digest="$(grep '^@ ' "$src_config")"
  dst_digest="$(grep '^@ ' "$dst_config")"
  [[ $src_digest != "$dst_digest" ]] || fail "dest digest must differ from source"

  # The blob was copied: readable through the new store.
  run_madder cat .dest "$hash"
  assert_success
  assert_line "migrate round trip"

  # The source store's config is byte-untouched.
  local src_after
  src_after="$(cat "$src_config")"
  [[ $src_before == "$src_after" ]] || fail "source config changed during migration"
}

function init_from_from_store_without_sync_copies_no_blobs { # @test
  init_store .source

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "config only" >"$blob"
  local hash
  hash="$(write_blob_id .source "$blob")"

  run_madder init-from --from-store .source .dest
  assert_success

  local dst_config=".madder/local/share/blob_stores/dest/blob_store-config"
  [[ -f $dst_config ]] || fail "dest config missing: $dst_config"

  # Without --sync the store exists but the source's blob was not copied.
  run_madder has .dest "$hash"
  assert_failure
}

function init_from_rejects_config_path_and_from_store { # @test
  init_store .source

  run_madder init-from --from-store .source .dest /some/config-path
  assert_failure
  assert_output --partial "mutually exclusive"
}

function init_from_rejects_pinned_id_with_from_store { # @test
  init_store .source

  local src_config=".madder/local/share/blob_stores/source/blob_store-config"
  local digest
  digest="$(grep '^@ ' "$src_config" | awk '{print $2}')"
  [[ -n $digest ]] || fail "could not read source config digest"

  run_madder init-from --from-store .source ".dest@$digest"
  assert_failure
  assert_output --partial "must not be digest-pinned"
}
