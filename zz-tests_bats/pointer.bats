setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=pointer

# init-pointer defaults to TomlPointerV1: a single-field config carrying
# only the absolute base-path of the target store. The resolver derives
# the config-path inside GetPath(); reads and writes routed through the
# pointer must materialize blobs at the target store's on-disk paths.
function init_pointer_default_is_v1 { # @test
  init_store .target

  # The leading '.' in the blob-store-id is the location-type prefix
  # (CWD-scoped); the on-disk directory name omits it.
  local target_base
  target_base="$PWD/.madder/local/share/blob_stores/target"
  [[ -d $target_base ]] || fail "target store base dir missing: $target_base"

  run_madder init-pointer -base-path "$target_base" .ptr
  assert_success

  local ptr_config=".madder/local/share/blob_stores/ptr/blob_store-config"
  [[ -f $ptr_config ]] || fail "expected pointer config at $ptr_config"

  # Wire-format pin: the v1 type-id must appear in the hyphence header,
  # the v1 field (base-path) must be present, and the v0-only fields
  # (id, config-path) must NOT.
  run cat "$ptr_config"
  assert_success
  assert_output --partial '! toml-blob_store_config-pointer-v1'
  assert_output --partial 'base-path = '
  refute_output --partial 'config-path'
  # 'id = ...' would be the v0 field; assert no such line exists.
  refute_line --regexp '^id = '
}

function init_pointer_v1_routes_writes_to_target { # @test
  init_store .target

  local target_base
  target_base="$PWD/.madder/local/share/blob_stores/target"

  run_madder init-pointer -base-path "$target_base" .ptr
  assert_success

  # Write through the pointer; read it back through the pointer.
  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "pointer-v1 round trip" >"$blob"

  local hash
  hash="$(write_blob_id .ptr "$blob")"
  [[ -n $hash ]] || fail "write through pointer returned no blob id"

  run_madder cat .ptr "$hash"
  assert_success
  assert_output --partial "pointer-v1 round trip"

  # The blob must physically live under the TARGET's base dir, not the
  # pointer's. This is the load-bearing assertion: it proves GetPath()
  # returned a path rooted at base-path (not at the pointer's own dir)
  # and the resolver dereferenced correctly.
  local target_entries
  target_entries="$(find "$target_base" -type f ! -name 'blob_store-config' | wc -l | tr -d ' ')"
  [[ $target_entries -ge 1 ]] || fail "expected at least one blob under target $target_base, got $target_entries"

  # Conversely, the pointer's own dir should hold only its config — no
  # blobs leaked into the wrong location.
  local ptr_base=".madder/local/share/blob_stores/ptr"
  local ptr_entries
  ptr_entries="$(find "$ptr_base" -type f ! -name 'blob_store-config' | wc -l | tr -d ' ')"
  [[ $ptr_entries -eq 0 ]] || fail "blobs leaked into pointer dir $ptr_base"
}

function init_pointer_v1_info_repo_blob_store_type { # @test
  # blob-store-type surfaces the pointer's own type tag, not the
  # target's — info-repo reads the on-disk config blob, which IS the
  # pointer's TomlPointerV1. Pins the GetBlobStoreType() return value
  # so a future rename doesn't silently change what info-repo reports.
  init_store .target

  local target_base
  target_base="$PWD/.madder/local/share/blob_stores/target"

  run_madder init-pointer -base-path "$target_base" .ptr
  assert_success

  run_madder info-repo .ptr blob-store-type
  assert_success
  assert_output 'local-pointer-v1'
}

function init_pointer_v0_still_available { # @test
  # The v0 subcommand remains for callers that need to write the legacy
  # three-field wire format. Pins both the on-disk tag and the presence
  # of the v0-only fields.
  init_store .target

  local target_base
  target_base="$PWD/.madder/local/share/blob_stores/target"
  local target_config="$target_base/blob_store-config"

  run_madder init-pointer-v0 \
    -id .target \
    -base-path "$target_base" \
    -config-path "$target_config" \
    .ptr_v0
  assert_success

  local ptr_config=".madder/local/share/blob_stores/ptr_v0/blob_store-config"
  [[ -f $ptr_config ]] || fail "expected v0 pointer config at $ptr_config"

  run cat "$ptr_config"
  assert_success
  assert_output --partial '! toml-blob_store_config-pointer-v0'
  assert_output --partial 'id = '
  assert_output --partial 'base-path = '
  assert_output --partial 'config-path = '
}
