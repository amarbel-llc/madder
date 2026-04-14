setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=pack

create_archive_config() {
  local store_name="$1"
  local delta_enabled="$2"
  local config_dir=".madder/local/share/blob_stores/${store_name}"

  mkdir -p "$config_dir"

  cat >"${config_dir}/dodder-blob_store-config" <<-'HEADER'
	---
	! toml-blob_store_config-inventory_archive-v1
	---
HEADER

  cat >>"${config_dir}/dodder-blob_store-config" <<-EOM

	hash_type-id = "blake2b256"
	compression-type = "zstd"
	loose-blob-store-id = ".default"
	encryption = ""
	max-pack-size = 0

	[delta]
	enabled = ${delta_enabled}
	algorithm = "bsdiff"
	min-blob-size = 0
	max-blob-size = 0
	size-ratio = 0.0
EOM
}

shared_blob_prefix() {
  local i
  for i in $(seq 1 50); do
    echo "shared content line ${i} with some padding to make it large enough for delta compression"
  done
}

function pack_with_delta { # @test
  skip "flag parsing or encryption bug — investigating (madder#2)"
  init_store
  create_archive_config "archive" "true"

  local prefix
  prefix="$(shared_blob_prefix)"

  local blob1="$BATS_TEST_TMPDIR/blob1.txt"
  local blob2="$BATS_TEST_TMPDIR/blob2.txt"
  local blob3="$BATS_TEST_TMPDIR/blob3.txt"

  printf '%s\nunique suffix alpha\n' "$prefix" >"$blob1"
  printf '%s\nunique suffix beta\n' "$prefix" >"$blob2"
  printf '%s\nunique suffix gamma\n' "$prefix" >"$blob3"

  local hash1 hash2 hash3

  run_madder write "$blob1"
  assert_success
  hash1="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash1 ]] || fail "write returned empty hash for blob one"

  run_madder write "$blob2"
  assert_success
  hash2="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder write "$blob3"
  assert_success
  hash3="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run timeout --preserve-status 10s madder pack
  assert_success

  run_madder cat "$hash1"
  assert_success
  assert_output --partial "unique suffix alpha"

  run_madder cat "$hash2"
  assert_success
  assert_output --partial "unique suffix beta"

  run_madder cat "$hash3"
  assert_success
  assert_output --partial "unique suffix gamma"

  run find .madder/local/share/blob_stores/archive -name '*.inventory_archive-v1' -type f
  assert_success
  assert_output
}

function pack_without_delta { # @test
  skip "flag parsing or encryption bug — investigating (madder#2)"
  init_store
  create_archive_config "archive" "false"

  local blob1="$BATS_TEST_TMPDIR/blob1.txt"
  local blob2="$BATS_TEST_TMPDIR/blob2.txt"

  echo "no delta alpha" >"$blob1"
  echo "no delta beta" >"$blob2"

  run_madder write "$blob1"
  assert_success
  hash1="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder write "$blob2"
  assert_success
  hash2="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run timeout --preserve-status 10s madder pack
  assert_success

  run_madder cat "$hash1"
  assert_success
  assert_output "no delta alpha"

  run_madder cat "$hash2"
  assert_success
  assert_output "no delta beta"

  run find .madder/local/share/blob_stores/archive -name '*.inventory_archive-v1' -type f
  assert_success
  assert_output
}
