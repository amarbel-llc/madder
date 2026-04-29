setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/sftp.bash"
  export output
  start_sftp_server
}

teardown() {
  stop_sftp_server
}

# bats file_tags=net_cap

today_sftp_session_file() {
  local date
  date="$(date -u +%Y-%m-%d)"
  local day_dir="$XDG_LOG_HOME/madder/inventory_log/$date"

  if [[ ! -d $day_dir ]]; then
    return 1
  fi

  ls -1 "$day_dir"/*.hyphence 2>/dev/null | head -n 1
}

# session_body strips the 4-line hyphence header (---, ! type, ---,
# blank separator) from a session file, leaving just the NDJSON body.
sftp_session_body() {
  tail -n +5 "$1"
}

function sftp_write_emits_written_record { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  # Per `madder write`: a leading blob-store-id arg switches the active
  # store for subsequent file args.
  run_madder write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_session_file)" || fail "no session file under $XDG_LOG_HOME/madder/inventory_log/"
  [[ -s $log ]] || fail "expected non-empty session file at $log"

  local body
  body="$(sftp_session_body "$log")"

  local n
  n="$(echo "$body" | grep -c '"op":"written"' || true)"
  [[ $n -eq 1 ]] || fail "expected 1 written record, got $n. body:
$body"
}

function sftp_write_disabled_by_no_inventory_log_flag { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  # --no-inventory-log is a global flag; place it before the subcommand.
  local bin="${MADDER_BIN:-madder}"
  run timeout --preserve-status 5s "$bin" --no-inventory-log write .sftp-test "$blob"
  assert_success

  local day_dir
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$(date -u +%Y-%m-%d)"
  if [[ -d $day_dir ]]; then
    local count
    count="$(ls -1 "$day_dir"/*.hyphence 2>/dev/null | wc -l)"
    [[ $count -eq 0 ]] || fail "--no-inventory-log should prevent session file creation; found $count file(s) in $day_dir"
  fi
}

function sftp_write_disabled_by_env_var { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello sftp" >"$blob"

  MADDER_INVENTORY_LOG=0 run_madder write .sftp-test "$blob"
  assert_success

  local day_dir
  day_dir="$XDG_LOG_HOME/madder/inventory_log/$(date -u +%Y-%m-%d)"
  if [[ -d $day_dir ]]; then
    local count
    count="$(ls -1 "$day_dir"/*.hyphence 2>/dev/null | wc -l)"
    [[ $count -eq 0 ]] || fail "MADDER_INVENTORY_LOG=0 should prevent session file creation; found $count file(s) in $day_dir"
  fi
}

function sftp_write_and_cat { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "hello world" >"$blob"

  run_madder write -format tap .sftp-test "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write returned empty hash. output: $output"

  run_madder cat .sftp-test "$hash"
  assert_success
  assert_output --partial "hello world"
}

function sftp_write_from_stdin { # @test
  init_sftp_test_store

  run_madder write -format tap .sftp-test -
  assert_success
}

function sftp_list_after_write { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "list test" >"$blob"

  run_madder write -format tap .sftp-test "$blob"
  assert_success

  run_madder list
  assert_success
  assert_output
}

function sftp_write_warns_when_file_shadows_store { # @test
  # Init an SFTP store named `shadowed`, then create a file with the
  # same bare name in CWD. Bare `write shadowed` should resolve to the
  # file but warn about the blob-store-id collision — same semantics
  # as the local-store version, since shadow detection is name-based
  # and backend-agnostic.
  init_sftp_test_store "$BATS_TEST_TMPDIR/sftp-shadowed" shadowed

  echo "file content" >shadowed

  run_madder write -format tap shadowed
  assert_success
  assert_output --partial "shadows blob-store-id"
  assert_output --partial "'./shadowed'"
  # Unlike `init -encryption none shadowed`, which yields a CWD-scoped
  # `.shadowed` store id in the warning, init-sftp-explicit registers
  # the id at user (XDG) scope where the bare name is the canonical
  # form — the warning quotes it as "shadowed".
  assert_output --partial '"shadowed"'
}

function sftp_write_no_warning_when_no_store_collision { # @test
  # Control: SFTP store id does not collide with any file in CWD;
  # write of an unrelated file must not surface a shadow warning.
  init_sftp_test_store

  echo "file content" >unique_filename

  run_madder write -format tap unique_filename
  assert_success
  refute_output --partial "shadows blob store"
}

function sftp_write_json_emits_ndjson_record { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'hello world\n' >"$blob"

  run_madder write -format json .sftp-test "$blob"
  assert_success
  # Non-default store, so the record carries a "store" field in
  # addition to id/size/source. Field order is a commitment of the
  # write contract. Use assert_line --regexp because $output
  # interleaves the SFTP transport's dialing/connecting log lines
  # with the NDJSON record; the local write_json_emits_ndjson_record
  # version has clean output and so can anchor at $output level.
  assert_line --regexp '^\{"id":"[^"]+","size":12,"source":"[^"]+blob\.txt","store":"\.sftp-test"\}$'
}

function sftp_write_json_check_present { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'already here\n' >"$blob"
  run_madder write -format json .sftp-test "$blob"
  assert_success

  run_madder write -check -format json .sftp-test "$blob"
  assert_success
  assert_output --partial '"present":true'
}

function sftp_write_json_check_missing { # @test
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'not stored\n' >"$blob"

  run_madder write -check -format json .sftp-test "$blob"
  assert_failure
  assert_output --partial '"present":false'
}

function sftp_write_json_warning_goes_to_stderr { # @test
  # Shadow warnings are informational; in JSON mode they must route
  # to stderr so stdout stays valid NDJSON. bats `run` merges streams
  # but the count of NDJSON records on the merged output should still
  # be exactly one — proves the warning didn't pollute stdout.
  init_sftp_test_store "$BATS_TEST_TMPDIR/sftp-shadowed" shadowed

  echo "file content" >shadowed

  run_madder write -format json shadowed
  assert_success
  local n
  n="$(echo "$output" | grep -c '^{' || true)"
  [[ $n -eq 1 ]] || fail "expected 1 NDJSON record, got $n. output:
$output"
  assert_output --partial 'shadows blob-store-id'
}

function sftp_init_idempotent_fails { # @test
  init_sftp_test_store

  # A second init for the same id should fail because the local
  # config file already exists; this matches the local-store
  # init_idempotent_fails contract.
  run_madder init-sftp-explicit \
    -host 127.0.0.1 \
    -port "$SFTP_PORT" \
    -user testuser \
    -password anything \
    -remote-path "$BATS_TEST_TMPDIR/sftp-remote" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    .sftp-test
  assert_failure
}

function sftp_init_compression_default { # @test
  # Mirrors init_compression_default for SFTP. The local test uses
  # assert_output (full-output equality) but the SFTP transport emits
  # dialing/connecting log lines that bats' merged run output captures
  # alongside the value, so anchor on assert_line for the printed
  # info-repo value instead. Per ADR 0005 / #60: compression-type is a
  # blob-store property and must come from the remote TomlV3.
  init_sftp_test_store

  run_madder info-repo .sftp-test compression-type
  assert_success
  assert_line 'zstd'
}

function sftp_init_hash_type_id_default { # @test
  # SFTP-init bootstrap writes a remote TomlV3 with HashTypeDefault
  # (blake2b256). Per ADR 0005 / #60, info-repo must surface that
  # (and not the stub sha256 the local TomlSFTPV0 returns).
  init_sftp_test_store

  run_madder info-repo .sftp-test hash_type-id
  assert_success
  assert_line 'blake2b256'
}

function sftp_init_hash_buckets_default { # @test
  # hash_buckets is not in the local SFTP transport config at all —
  # it's a blob-store property that lives only on the remote TomlV3.
  # Without #60's GetBlobStoreConfig() fall-through this would fail
  # with "unsupported info key".
  init_sftp_test_store

  run_madder info-repo .sftp-test hash_buckets
  assert_success
  assert_line '[2]'
}

function sftp_info_repo_host_stays_local { # @test
  # Reading transport keys (host, port, user, …) on an SFTP store must
  # not open the SSH/SFTP connection. Backend-property keys
  # (compression-type, hash_type-id) still do.
  init_sftp_test_store

  run_madder info-repo .sftp-test host
  assert_success
  assert_line '127.0.0.1'
  refute_output --partial 'dialing'
  refute_output --partial 'connected to'
}

function sftp_info_repo_config_immutable_encodes_remote { # @test
  # Per ADR 0005 §"info-repo … config-immutable wire shape", the
  # pseudo-key must encode the remote blob-store config, not the local
  # SFTP transport. The refute_output lines pin against an encoder
  # regression that falls back to local.
  init_sftp_test_store

  run_madder info-repo .sftp-test config-immutable
  assert_success
  assert_output --partial 'compression-type'
  assert_output --partial 'zstd'
  assert_output --partial 'hash_type-id'
  assert_output --partial 'blake2b256'
  refute_output --partial 'private-key-path'
  refute_output --partial 'remote-path'
}

function sftp_init_remote_config_is_read_only { # @test
  # Per ADR 0005, blob_store-config files are immutable per store
  # identity. #65 enforces this on disk via 0o444 mode bits, on the
  # SFTP remote as well as locally.
  init_sftp_test_store

  # The test SFTP server re-roots absolute paths under its CWD
  # ($BATS_TEST_TMPDIR); the remote config lands inside that subtree.
  local remote_config
  remote_config="$(find "$BATS_TEST_TMPDIR" -type f -name 'blob_store-config' \
    -path "*sftp-remote*" -print -quit)"
  [[ -n $remote_config ]] || fail "no remote blob_store-config found"

  local mode
  mode="$(file_mode "$remote_config")"
  [[ $mode == '444' ]] || fail "expected mode 444 on $remote_config; got $mode"
}

function sftp_write_compresses_per_remote_config { # @test
  # Per ADR 0005, the remote blob_store-config dictates on-wire shape.
  # init_sftp_test_store provisions zstd, so a published blob's bytes
  # MUST start with the zstd magic. If the IO wrapper falls back to
  # compression-none, the bytes equal the plaintext and this fails.
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  printf 'compress me please' >"$blob"

  run_madder write .sftp-test "$blob"
  assert_success

  # The test SFTP server re-roots absolute paths under its CWD
  # ($BATS_TEST_TMPDIR); locate the blob by hash-bucket layout.
  local on_disk
  on_disk="$(find "$BATS_TEST_TMPDIR" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_disk ]] || fail "no blob file found under $BATS_TEST_TMPDIR"

  local magic
  magic="$(xxd -p -l 4 "$on_disk")"
  [[ $magic == '28b52ffd' ]] || fail "expected zstd magic at start of $on_disk; got $magic"
}

function sftp_cross_hash_sync { # @test
  # Mirrors cross_hash_sync (sync.bats) for the SFTP transport: write a
  # blob into a local blake2b256 store, then sync into an SFTP-backed
  # store and confirm the blob materializes there.
  init_store
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "cross-hash-sftp-test" >"$blob"

  run_madder write -format tap "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep -oP 'blake2b256-\S+' | head -1)"
  [[ -n $blob_id ]] || fail "write returned empty blob_id: $output"

  run_madder sync .default .sftp-test
  assert_success

  run_madder cat .sftp-test "$blob_id"
  assert_success
  assert_line "cross-hash-sftp-test"
}

function sftp_sync_idempotent { # @test
  # Mirrors sync_idempotent (sync.bats): a second sync over an
  # already-synced blob is a no-op refusal-free success.
  init_store
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sftp-idempotent-test" >"$blob"
  run_madder write "$blob"
  assert_success

  run_madder sync .default .sftp-test
  assert_success

  run_madder sync .default .sftp-test
  assert_success
}

function sftp_sync_json_auto_detects { # @test
  # Mirrors sync_json_auto_detects (sync.bats): NDJSON state records
  # round-trip through the SFTP transport.
  init_store
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sftp-sync-json-test" >"$blob"
  run_madder write -format tap "$blob"
  assert_success

  run_madder sync .default .sftp-test
  assert_success
  assert_output --partial '"state":"transferred"'
  refute_output --partial 'TAP version 14'
}

function sftp_fsck_with_blobs { # @test
  # Mirrors with_blobs (fsck.bats): a healthy SFTP store with blobs
  # fsck-passes without a 'not ok' line.
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "fsck sftp content" >"$blob"
  run_madder write -format tap .sftp-test "$blob"
  assert_success

  run_madder fsck -format tap .sftp-test
  assert_success
  assert_output --partial "TAP version 14"
  refute_output --partial "not ok"
}

function sftp_fsck_json_auto_detects { # @test
  # Mirrors fsck_json_auto_detects (fsck.bats) for SFTP: piped stdout
  # auto-selects NDJSON; verified records are emitted.
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "sftp fsck json" >"$blob"
  run_madder write -format tap .sftp-test "$blob"
  assert_success

  run_madder fsck .sftp-test
  assert_success
  assert_output --partial '"state":"verified"'
  refute_output --partial 'TAP version 14'
}

function sftp_fsck_tap14_output { # @test
  # Symmetric to tap14_output (fsck.bats): an empty SFTP store
  # fsck-passes cleanly under tap formatting.
  init_sftp_test_store

  run_madder fsck -format tap .sftp-test
  assert_success
  assert_output --partial "TAP version 14"
  assert_output --partial "1.."
  refute_output --partial "not ok"
}

function sftp_fsck_json_reports_missing { # @test
  # Mirrors fsck_json_reports_missing (fsck.bats) for SFTP. Delete
  # the remote blob files (re-rooted under $BATS_TEST_TMPDIR by the
  # test SFTP server) so AllBlobs sees the IDs but HasBlob fails.
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "blob to remove" >"$blob"
  run_madder write .sftp-test "$blob"
  assert_success

  find "$BATS_TEST_TMPDIR/sftp-remote" -type f -path '*/blake2b256/*' -delete 2>/dev/null || true

  run_madder fsck -format json .sftp-test
  assert_output --partial '"store":'
}

# create_sftp_archive_config writes a local inventory_archive
# blob_store-config that pulls loose blobs from the SFTP store. The
# local archive store stays on the host filesystem; only the loose
# source is SFTP-backed. Mirrors create_archive_config in pack.bats.
create_sftp_archive_config() {
  local store_name="$1"
  local delta_enabled="$2"
  local config_dir=".madder/local/share/blob_stores/${store_name}"

  mkdir -p "$config_dir"

  cat >"${config_dir}/blob_store-config" <<-'HEADER'
	---
	! toml-blob_store_config-inventory_archive-v1
	---
HEADER

  cat >>"${config_dir}/blob_store-config" <<-EOM

	hash_type-id = "blake2b256"
	compression-type = "zstd"
	loose-blob-store-id = ".sftp-test"
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

function sftp_pack_with_delta { # @test
  # Mirrors pack_with_delta (pack.bats) with the loose source on
  # SFTP. The archive itself stays local; pack iterates the SFTP
  # loose store via AllBlobs to collect candidates.
  init_sftp_test_store
  create_sftp_archive_config "archive" "true"

  local blob1="$BATS_TEST_TMPDIR/blob1.txt"
  local blob2="$BATS_TEST_TMPDIR/blob2.txt"
  local prefix
  prefix="$(for i in $(seq 1 50); do
    echo "shared content line ${i} with some padding to make it large enough for delta compression"
  done)"

  printf '%s\nunique suffix alpha\n' "$prefix" >"$blob1"
  printf '%s\nunique suffix beta\n' "$prefix" >"$blob2"

  local hash1 hash2

  run_madder write -format tap .sftp-test "$blob1"
  assert_success
  hash1="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder write -format tap .sftp-test "$blob2"
  assert_success
  hash2="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run timeout --preserve-status 10s "$MADDER_BIN" pack
  assert_success

  run_madder cat "$hash1"
  assert_success
  assert_output --partial "unique suffix alpha"

  run_madder cat "$hash2"
  assert_success
  assert_output --partial "unique suffix beta"

  run find .madder/local/share/blob_stores/archive -name '*.inventory_archive-v1' -type f
  assert_success
  assert_output
}

function sftp_pack_without_delta { # @test
  # Mirrors pack_without_delta (pack.bats) with the loose source on SFTP.
  init_sftp_test_store
  create_sftp_archive_config "archive" "false"

  local blob1="$BATS_TEST_TMPDIR/blob1.txt"
  local blob2="$BATS_TEST_TMPDIR/blob2.txt"

  echo "no delta alpha" >"$blob1"
  echo "no delta beta" >"$blob2"

  local hash1 hash2

  run_madder write -format tap .sftp-test "$blob1"
  assert_success
  hash1="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run_madder write -format tap .sftp-test "$blob2"
  assert_success
  hash2="$(echo "$output" | grep '^ok ' | awk '{print $4}')"

  run timeout --preserve-status 10s "$MADDER_BIN" pack
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

function sftp_init_with_encryption { # @test
  # Mirrors init_with_encryption (init.bats) for SFTP. ADR 0005 routes
  # the -encryption flag to the remote TomlV3, so info-repo must surface
  # a non-empty markl-id and a blob round-trip must work end-to-end.
  # Add an on-disk check (mirroring sftp_write_compresses_per_remote_config)
  # so a regression that silently no-ops the IO wrapper would still fail.
  local remote_root="$BATS_TEST_TMPDIR/sftp-encrypted"
  run_madder init-sftp-explicit \
    -host 127.0.0.1 \
    -port "$SFTP_PORT" \
    -user testuser \
    -password anything \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    -encryption generate \
    .sftp-encrypted
  assert_success

  run_madder info-repo .sftp-encrypted encryption
  assert_success
  assert_output --regexp '.+'

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "encrypted-sftp-roundtrip" >"$blob"

  run_madder write -format tap .sftp-encrypted "$blob"
  assert_success
  local hash
  hash="$(echo "$output" | grep '^ok ' | awk '{print $4}')"
  [[ -n $hash ]] || fail "write returned empty hash. output: $output"

  run_madder cat .sftp-encrypted "$hash"
  assert_success
  assert_output --partial "encrypted-sftp-roundtrip"

  # On-disk bytes MUST NOT contain the cleartext marker. If the IO
  # wrapper silently fell back to plaintext, this assertion would
  # catch the regression even though the round-trip above succeeds.
  # The test SFTP server re-roots absolute paths under its CWD
  # ($BATS_TEST_TMPDIR), matching the find pattern used in
  # sftp_write_compresses_per_remote_config.
  local on_disk
  on_disk="$(find "$BATS_TEST_TMPDIR" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_disk ]] || fail "no blob file found under $BATS_TEST_TMPDIR"

  if grep -q 'encrypted-sftp-roundtrip' "$on_disk"; then
    fail "expected encrypted bytes on disk; found cleartext at $on_disk"
  fi
}

function sftp_init_discover_rejects_encryption { # @test
  # Per #98, `init-sftp-explicit -discover -encryption` is refused at
  # flag-handling time: -discover adopts the existing remote layout,
  # -encryption requires a fresh store, and combining them would
  # produce a half-encrypted store with fragmented decryption. The
  # bootstrap path (without -discover) handles -encryption correctly
  # in #57.
  local remote_root="$BATS_TEST_TMPDIR/sftp-discover-encryption"
  run_madder init-sftp-explicit \
    -discover \
    -host 127.0.0.1 \
    -port "$SFTP_PORT" \
    -user testuser \
    -password anything \
    -remote-path "$remote_root" \
    -known-hosts-file "$SFTP_KNOWN_HOSTS" \
    -encryption generate \
    .sftp-discover-enc
  assert_failure
  assert_output --partial '-encryption cannot be combined with -discover'
}

function sftp_init_without_encryption { # @test
  # Symmetric to init_without_encryption (init.bats:42). With no
  # -encryption flag, the remote config-immutable view emits an empty
  # encryption list. Use config-immutable rather than `info-repo
  # encryption` because under bats `run` stderr is merged with stdout,
  # so dialing log lines confound a plain `assert_output ''` check.
  init_sftp_test_store

  run_madder info-repo .sftp-test config-immutable
  assert_success
  assert_output --partial 'encryption = []'
}

function sftp_write_record_has_contracted_fields { # @test
  export XDG_LOG_HOME="$BATS_TEST_TMPDIR/xdg-log"
  init_sftp_test_store

  local blob="$BATS_TEST_TMPDIR/blob.txt"
  echo "schema check" >"$blob"

  run_madder write .sftp-test "$blob"
  assert_success

  local log
  log="$(today_sftp_session_file)" || fail "no session file"
  local line
  line="$(sftp_session_body "$log" | head -n 1)"

  # Every field the design contracts is present. The description field
  # is optional (omitempty) and expected to be absent here since
  # --log-description is not passed.
  echo "$line" | grep -q '"type":"blob-write-published-v1"' || fail "record missing type discriminator: $line"
  echo "$line" | grep -q '"ts":' || fail "record missing ts field: $line"
  echo "$line" | grep -q '"utility":"madder"' || fail "record utility != madder: $line"
  echo "$line" | grep -q '"pid":' || fail "record missing pid field: $line"
  echo "$line" | grep -q '"store_id":' || fail "record missing store_id: $line"
  echo "$line" | grep -q '"markl_id":' || fail "record missing markl_id: $line"
  echo "$line" | grep -q '"size":' || fail "record missing size field: $line"
  echo "$line" | grep -q '"op":"written"' || fail "record op != written: $line"

  echo "$line" | grep -q '"description"' &&
    fail "description field should be absent when --log-description not passed: $line" ||
    true
}
