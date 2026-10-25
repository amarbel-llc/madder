setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/piv_agent.bash"
  export output
  start_piv_agent
  require_bin MADDER_STORE_CLI madder-store-cli

  # The pigpen names two recipients: the virtual card's slot-9D key, so the
  # Go madder can open the store key through the real agent, and an X25519
  # recipient the Rust CLI mints, so the Rust side can open it with a
  # software identity and no agent at all.
  local minted
  minted="$("$MADDER_STORE_CLI" mint-recipient)"
  RUST_RECIPIENT="$(sed -n 1p <<<"$minted")"
  export MADDER_STORE_IDENTITY_HEX
  MADDER_STORE_IDENTITY_HEX="$(sed -n 2p <<<"$minted")"
  [[ $RUST_RECIPIENT == piggy-recipient-v1@age_x25519_pub-* ]] ||
    fail "mint-recipient printed no recipient: $minted"

  PIGPEN="$BATS_TEST_TMPDIR/piggy-ids"
  printf '%s\n%s\n' "$PIV_RECIPIENT_ID" "$RUST_RECIPIENT" >"$PIGPEN"
}

teardown() {
  stop_piv_agent
}

# bats file_tags=rust_interop

# Cross-language conformance for the Rust crate crates/madder-store (design:
# docs/plans/2026-10-25-rust-sealed-key-store-design.md): what the Go madder
# writes into a sealed-key store, Rust reads back byte-exact; what Rust
# writes, `madder cat` and `madder fsck` read and verify; a store Rust
# initializes, Go lists, writes to and reads. The Rust CLI speaks madder's
# grammar (so pigpen_store.bats's `rust_cli` tests also run against it);
# here the two binaries work on ONE store. The Go->Rust half also runs
# offline against committed fixtures in `cargo test`.

run_madder_agent() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

run_madder_no_agent() {
  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

run_rust() {
  run timeout --preserve-status 20s "$MADDER_STORE_CLI" "$@"
}

store_dir() {
  echo ".madder/local/share/blob_stores/${1:-sealed}"
}

init_sealed_store() {
  run_madder_agent init -pigpen "$PIGPEN" "$@" .sealed
  assert_success
}

# tap_blob_id echoes the blob id of the first `ok` line in $output.
tap_blob_id() {
  echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1
}

# go_write_blob writes a one-line blob with the Go madder, no agent, and
# echoes its id.
go_write_blob() {
  local blob="$BATS_TEST_TMPDIR/go-$RANDOM.txt"
  echo "$1" >"$blob"
  run_madder_no_agent write -format tap .sealed "$blob"
  assert_success
  local blob_id
  blob_id="$(tap_blob_id)"
  [[ -n $blob_id ]] || fail "madder write returned no blob id. output: $output"
  echo "$blob_id"
}

# rust_write_blob writes a file with the Rust CLI and echoes its id.
rust_write_blob() {
  run_rust write -format tap "${2:-.sealed}" "$1"
  assert_success
  local blob_id
  blob_id="$(tap_blob_id)"
  [[ -n $blob_id ]] || fail "rust write returned no blob id. output: $output"
  echo "$blob_id"
}

card_ecdh_count() {
  grep -c 'GA ECDH 9D' "$PIV_FIBBY_LOG" || true
}

function rust_reports_the_sealed_recipients_without_a_key { # @test
  init_sealed_store

  run env -u MADDER_STORE_IDENTITY_HEX "$MADDER_STORE_CLI" key-status .sealed
  assert_success
  assert_output --partial 'sealed to:   2 recipients ('
  assert_output --partial "pigpen:      $PIGPEN (path)"
  assert_output --partial 'pigpen now:  2 recipients ('
  assert_output --partial 'status:      in sync'

  [[ "$(card_ecdh_count)" == "0" ]] || fail "key-status used the card"
}

function rust_reads_what_go_wrote { # @test
  init_sealed_store

  local first second
  first="$(go_write_blob "written by go, read by rust")"
  second="$(go_write_blob "a second blob")"

  run_rust cat-ids .sealed
  assert_success
  assert_line "$first"
  assert_line "$second"

  run_rust cat .sealed "$first" "$second"
  assert_success
  assert_output --partial 'written by go, read by rust'
  assert_output --partial 'a second blob'

  # The Rust side opened the key with its X25519 identity: no card.
  [[ "$(card_ecdh_count)" == "0" ]] || fail "the Rust reader used the card"

  # Without the identity (and no agent socket) it cannot read, and says so
  # the way Go does.
  run env -u MADDER_STORE_IDENTITY_HEX -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    "$MADDER_STORE_CLI" cat .sealed "$first"
  assert_failure
  refute_output --partial 'written by go'
  assert_output --partial 'no agent socket'
  assert_output --partial '1 blob(s) could not be read'
}

function go_reads_what_rust_wrote { # @test
  init_sealed_store

  local plain="$BATS_TEST_TMPDIR/rust.txt"
  echo "written by rust, read by go" >"$plain"
  # A larger blob too: several age STREAM chunks and more than one zstd block.
  local large="$BATS_TEST_TMPDIR/rust-large.bin"
  head -c 300000 /dev/urandom >"$large"

  local small_id large_id
  small_id="$(rust_write_blob "$plain")"
  large_id="$(rust_write_blob "$large")"
  [[ $small_id == blake2b256-* && $large_id == blake2b256-* ]] ||
    fail "rust write printed no ids: $small_id $large_id"

  # Published read-only, as Go publishes.
  local on_disk
  on_disk="$(find "$(store_dir)" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_disk ]] || fail "no blob file found"
  [[ "$(file_mode "$on_disk")" == "444" ]] || fail "blob mode is $(file_mode "$on_disk"), want 444"
  run head -n 1 "$on_disk"
  assert_output 'age-encryption.org/v1'

  # Go prints a store-switch notice on stderr beside the content, so the
  # assertions are per line, and the binary blob goes through a file.
  run_madder_agent cat .sealed "$small_id"
  assert_success
  assert_line 'written by rust, read by go'

  local large_out="$BATS_TEST_TMPDIR/rust-large.out"
  timeout --preserve-status 20s "${MADDER_BIN:-madder}" cat .sealed "$large_id" >"$large_out" 2>/dev/null ||
    fail "madder cat of the large blob failed"
  cmp "$large" "$large_out" || fail "large blob differs after the Go read"

  run_madder_agent fsck -format ndjson .sealed
  assert_success
  assert_output --partial '"state":"verified"'
  assert_output --partial 'blobs verified: 2'
  refute_output --partial 'blobs failed'
}

function both_sides_mint_the_same_id_for_the_same_bytes { # @test
  init_sealed_store

  local plain="$BATS_TEST_TMPDIR/same.txt"
  echo "the same bytes" >"$plain"

  local rust_id go_id
  rust_id="$(rust_write_blob "$plain")"

  run_madder_no_agent write -format tap .sealed "$plain"
  assert_success
  go_id="$(tap_blob_id)"

  [[ $rust_id == "$go_id" ]] || fail "ids differ: rust $rust_id, go $go_id"

  # One blob on disk: the second writer hit link(2)'s EEXIST.
  run_madder_no_agent cat-ids .sealed
  assert_success
  assert_line "$go_id"
  [[ "$(grep -c '^blake2b256-' <<<"$output")" == "1" ]] || fail "expected one blob id: $output"
}

function go_reads_a_store_rust_initialized { # @test
  # pigsty on Android has no Go madder, so the Rust crate creates stores
  # too. Go must see the store, report its key status, write to it and
  # read what Rust wrote.
  run_rust init -pigpen "$PIGPEN" -pigpen-kind embedded .rusty
  assert_success
  assert_output --partial "ok 1 - init $PWD/$(store_dir rusty)/blob_store-config"

  local base
  base="$(store_dir rusty)"
  [[ -f $base/blob_store-config ]] || fail "no config at $base"
  [[ -f $base/blob_store-key ]] || fail "no sidecar at $base"
  run cat "$base/blob_store-config"
  assert_output --partial '! toml-blob_store_config-v5'
  assert_output --partial 'piggy-recipient-v1@age_x25519_pub-'
  assert_output --partial 'holder = "process"'
  refute_output --partial 'age_x25519_sec'

  run_madder_no_agent list
  assert_success
  assert_output --partial 'rusty'
  refute_output --partial 'unmigrated'

  run_madder_no_agent key-status .rusty
  assert_success
  assert_output --partial 'embedded in blob_store-key'
  assert_output --partial 'sealed to:   2 recipients ('
  assert_output --partial 'status:      in sync'

  local plain="$BATS_TEST_TMPDIR/into-rusty.txt"
  echo "go writes into a rust-made store" >"$plain"
  run_madder_no_agent write -format tap .rusty "$plain"
  assert_success
  local go_id
  go_id="$(tap_blob_id)"
  [[ -n $go_id ]] || fail "madder write returned no blob id. output: $output"

  run_rust cat .rusty "$go_id"
  assert_success
  assert_line 'go writes into a rust-made store'

  echo "rust writes into its own store" >"$plain"
  local rust_id
  rust_id="$(rust_write_blob "$plain" .rusty)"
  run_madder_agent cat .rusty "$rust_id"
  assert_success
  assert_line 'rust writes into its own store'

  run_madder_agent fsck -format ndjson .rusty
  assert_success
  assert_output --partial 'blobs verified: 2'
  refute_output --partial 'blobs failed'

  # A second init must not replace the key.
  run_rust init -pigpen "$PIGPEN" -pigpen-kind embedded .rusty
  assert_failure
  run_madder_agent cat .rusty "$rust_id"
  assert_success
}

function the_other_hash_and_no_compression_interoperate_too { # @test
  init_sealed_store -compression-type none -hash_type-id sha256

  local plain="$BATS_TEST_TMPDIR/sha.txt"
  echo "sha256, uncompressed" >"$plain"

  local id
  id="$(rust_write_blob "$plain")"
  [[ $id == sha256-* ]] || fail "expected a sha256 id, got $id"

  run_madder_agent cat .sealed "$id"
  assert_success
  assert_line 'sha256, uncompressed'

  local go_id
  go_id="$(go_write_blob "go, sha256")"
  run_rust cat .sealed "$go_id"
  assert_success
  assert_line 'go, sha256'
}

function plain_stores_interoperate_without_any_key { # @test
  # madder#318: the V4 stores `madder init -encryption none|generate`
  # writes, unencrypted or with a raw key in the config. Neither side needs
  # an identity or an agent for them. Go inits both shapes and Rust reads
  # and writes them; then Rust inits both shapes and Go reads and writes
  # them.
  run_madder_no_agent init -encryption none .plain
  assert_success
  run_madder_no_agent init -encryption generate .keyed
  assert_success

  local blob="$BATS_TEST_TMPDIR/plain.txt"
  local store go_id rust_id
  for store in .plain .keyed; do
    echo "go writes into $store, rust reads" >"$blob"
    run_madder_no_agent write -format tap "$store" "$blob"
    assert_success
    go_id="$(tap_blob_id)"
    [[ -n $go_id ]] || fail "madder write returned no blob id. output: $output"

    run env -u MADDER_STORE_IDENTITY_HEX -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
      "$MADDER_STORE_CLI" cat "$store" "$go_id"
    assert_success
    assert_line "go writes into $store, rust reads"

    echo "rust writes into $store, go reads" >"$blob"
    rust_id="$(rust_write_blob "$blob" "$store")"
    run_madder_no_agent cat "$store" "$rust_id"
    assert_success
    assert_line "rust writes into $store, go reads"

    run_madder_no_agent fsck -format ndjson "$store"
    assert_success
    assert_output --partial 'blobs verified: 2'
    refute_output --partial 'blobs failed'
  done

  # The raw-key store's blobs are age files; the unencrypted one's are not.
  local on_disk
  on_disk="$(find "$(store_dir keyed)" -type f -path '*/blake2b256/*' -print -quit)"
  run head -c 21 "$on_disk"
  assert_output 'age-encryption.org/v1'
  on_disk="$(find "$(store_dir plain)" -type f -path '*/blake2b256/*' -print -quit)"
  run head -c 21 "$on_disk"
  refute_output 'age-encryption.org/v1'

  run_rust init -encryption none .rplain
  assert_success
  run_rust init -encryption generate .rkeyed
  assert_success
  for store in .rplain .rkeyed; do
    run cat "$(store_dir "${store#.}")/blob_store-config"
    assert_output --partial '! toml-blob_store_config-v4'
    assert_line --regexp '^@ blake2b256-'
    [[ "$(file_mode "$(store_dir "${store#.}")/blob_store-config")" == "444" ]] ||
      fail "rust-written config is not 0444"

    echo "go writes into a rust-made $store" >"$blob"
    run_madder_no_agent write -format tap "$store" "$blob"
    assert_success
    go_id="$(tap_blob_id)"
    [[ -n $go_id ]] || fail "madder write returned no blob id. output: $output"
    run_rust cat "$store" "$go_id"
    assert_success
    assert_line "go writes into a rust-made $store"

    run_madder_no_agent fsck -format ndjson "$store"
    assert_success
    refute_output --partial 'blobs failed'
  done

  run_madder_no_agent info-repo .rplain encryption
  assert_success
  assert_output ''
  run_madder_no_agent info-repo .rkeyed encryption
  assert_success
  assert_output --partial 'madder-private_key-v1@age_x25519_sec-'
}
