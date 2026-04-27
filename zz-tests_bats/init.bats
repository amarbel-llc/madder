setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=init

function init_default { # @test
  init_store
}

function init_default_config_is_read_only { # @test
  # Per ADR 0005, blob_store-config files are immutable per store
  # identity. #65 enforces this on disk via 0o444 mode bits so vim or
  # concurrent writers can't silently mutate the store's identity.
  init_store

  local config=".madder/local/share/blob_stores/default/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"

  local mode
  # GNU stat in the nix devshell.
  mode="$(stat -c '%a' "$config")"
  [[ $mode == '444' ]] || fail "expected mode 444 on $config; got $mode"
}

function init_idempotent_fails { # @test

  init_store
  run_madder init -encryption none .default
  assert_failure
}

function init_compression_default { # @test

  init_store
  run_madder info-repo compression-type
  assert_output 'zstd'
}

function init_without_encryption { # @test

  init_store
  run_madder info-repo encryption
  assert_output ''
}

function init_with_encryption { # @test

  run_madder init -encryption generate .encrypted
  assert_success
  run_madder info-repo .encrypted encryption
  assert_success
  assert_output --regexp '.+'
}

function init_inventory_archive { # @test

  init_store
  run_madder init-inventory-archive -encryption none .archive
  assert_success
}

function init_inventory_archive_with_encryption { # @test

  init_store
  run_madder init-inventory-archive -encryption generate .archive
  assert_success
  run_madder info-repo .archive encryption
  assert_success
  assert_output --regexp '.+'
}

function init_error_includes_store_id_and_path { # @test

  # Regression for #21: when store discovery hits an invalid config on
  # disk, the error must identify which store and which config file.
  # Pre-populate a .broken store with hash_type-id = "", then try to
  # init a new store. Discovery will try to decode .broken first and
  # the error should name both the store id and the config path.

  local store_dir=".madder/local/share/blob_stores/broken"
  mkdir -p "$store_dir"

  cat >"$store_dir/blob_store-config" <<-'HEADER'
	---
	! toml-blob_store_config-v3
	---
HEADER

  cat >>"$store_dir/blob_store-config" <<-'EOM'

	hash_type-id = ""
	compression-type = "zstd"
	encryption = ""
	hash-buckets = [2]
EOM

  run_madder init -encryption none .default
  assert_failure
  assert_output --partial '".broken"'
  assert_output --partial "$store_dir/blob_store-config"
  assert_output --partial "unsupported hash type"
}
