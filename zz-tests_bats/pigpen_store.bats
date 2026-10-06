setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  load "$(dirname "$BATS_TEST_FILE")/lib/piv_agent.bash"
  export output
  start_piv_agent

  # A pigpen in its simplest form: one recipient per line. The recipient
  # is the virtual card's slot-9D key, so opening the store's sealed key
  # goes through the real piggy-agent.
  PIGPEN="$BATS_TEST_TMPDIR/piggy-ids"
  echo "$PIV_RECIPIENT_ID" >"$PIGPEN"
}

teardown() {
  stop_piv_agent
}

# bats file_tags=piv_agent

# Sealed-key stores (FDR 0011): `madder init -pigpen` mints an X25519
# store key and seals its secret half to the pigpen's recipients. Blobs
# are ordinary age files encrypted to the public half, so writing needs
# no agent; reading opens the sealed key once per process through the
# agent and decrypts every blob in software after that.

run_madder_agent() {
  run timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

run_madder_no_agent() {
  run env -u PIGGY_AUTH_SOCK -u SSH_AUTH_SOCK -u PIVY_AUTH_SOCK \
    timeout --preserve-status 20s "${MADDER_BIN:-madder}" "$@"
}

init_sealed_store() {
  run_madder_agent init -pigpen "$PIGPEN" .sealed
  assert_success
}

# write_sealed_blob writes a one-line blob to .sealed with NO agent
# reachable and echoes its id.
write_sealed_blob() {
  local blob="$BATS_TEST_TMPDIR/blob-$RANDOM.txt"
  echo "$1" >"$blob"
  run_madder_no_agent write -format tap .sealed "$blob"
  assert_success
  local blob_id
  blob_id="$(echo "$output" | grep '^ok ' | awk '{print $4}' | head -n 1)"
  [[ -n $blob_id ]] || fail "write returned no blob id. output: $output"
  echo "$blob_id"
}

sealed_store_dir() {
  echo ".madder/local/share/blob_stores/sealed"
}

card_ecdh_count() {
  grep -c 'GA ECDH 9D' "$PIV_FIBBY_LOG" || true
}

function init_pigpen_writes_no_secret { # @test
  init_sealed_store

  local config sidecar
  config="$(sealed_store_dir)/blob_store-config"
  sidecar="$(sealed_store_dir)/blob_store-key"
  [[ -f $config ]] || fail "no config at $config"
  [[ -f $sidecar ]] || fail "no sidecar at $sidecar"

  # The config is the sealed-key version and carries the PUBLIC key only.
  run cat "$config"
  assert_output --partial '! toml-blob_store_config-v5'
  assert_output --partial 'age_x25519_pub-'
  assert_output --partial 'holder = "process"'
  refute_output --partial 'age_x25519_sec'

  # The sidecar holds the sealed document and where its recipients came
  # from, and no secret in the clear either.
  run cat "$sidecar"
  assert_output --partial '! toml-blob_store_key-v2'
  assert_output --partial "$PIGPEN"
  assert_output --partial 'pigpen-v1'
  refute_output --partial 'age_x25519_sec'

  # Creating the store sealed the key in software: the card was not asked.
  [[ "$(card_ecdh_count)" == "0" ]] || fail "init used the card"
}

function sealed_store_writes_without_an_agent { # @test
  init_sealed_store

  write_sealed_blob "sealed-store-blob" >/dev/null

  local on_disk
  on_disk="$(find "$(sealed_store_dir)" -type f -path '*/blake2b256/*' -print -quit)"
  [[ -n $on_disk ]] || fail "no blob file found"
  if grep -q 'sealed-store-blob' "$on_disk"; then
    fail "expected encrypted bytes on disk; found cleartext at $on_disk"
  fi

  run head -n 1 "$on_disk"
  assert_output 'age-encryption.org/v1'

  [[ "$(card_ecdh_count)" == "0" ]] || fail "writing used the card"
}

function sealed_store_reads_many_blobs_with_one_card_operation { # @test
  init_sealed_store

  local first second third
  first="$(write_sealed_blob "first sealed blob")"
  second="$(write_sealed_blob "second sealed blob")"
  third="$(write_sealed_blob "third sealed blob")"

  run_madder_agent cat .sealed "$first" "$second" "$third"
  assert_success
  assert_output --partial 'first sealed blob'
  assert_output --partial 'second sealed blob'
  assert_output --partial 'third sealed blob'

  # Three blobs, one process: the sealed key was opened once, so the
  # card did exactly one ECDH. This is the property the design exists
  # for (a 91,500-blob fsck must not be 91,500 card operations).
  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for 3 blobs, got $(card_ecdh_count)"
}

function sealed_store_fsck_verifies_through_the_agent { # @test
  init_sealed_store

  write_sealed_blob "fsck one" >/dev/null
  write_sealed_blob "fsck two" >/dev/null

  run_madder_agent fsck -format ndjson .sealed
  assert_success
  assert_output --partial '"state":"verified"'
  assert_output --partial 'blobs verified: 2'
  refute_output --partial 'blobs failed'

  [[ "$(card_ecdh_count)" == "1" ]] ||
    fail "expected 1 card ECDH for the fsck, got $(card_ecdh_count)"
}

function sealed_store_without_an_agent_is_unreadable_not_missing { # @test
  init_sealed_store

  local blob_id
  blob_id="$(write_sealed_blob "needs the agent")"

  run_madder_no_agent cat .sealed "$blob_id"
  assert_failure
  refute_output --partial 'needs the agent'
  assert_output --partial 'no agent socket'
  assert_output --partial '1 blob(s) could not be read'
  refute_output --partial 'not found'

  run_madder_no_agent fsck -format ndjson .sealed
  assert_failure
  assert_output --partial '"state":"read_error"'
  refute_output --partial '"state":"corrupt"'
}

function init_pigpen_refuses_bad_input_and_creates_nothing { # @test
  # A pigpen that does not exist.
  run_madder_agent init -pigpen "$BATS_TEST_TMPDIR/no-such-pigpen" .sealed
  assert_failure
  [[ ! -e "$(sealed_store_dir)" ]] || fail "a store was created from a missing pigpen"

  # A pigpen naming no recipients: a key sealed to nobody could never be
  # opened.
  : >"$BATS_TEST_TMPDIR/empty-pigpen"
  run_madder_agent init -pigpen "$BATS_TEST_TMPDIR/empty-pigpen" .sealed
  assert_failure
  [[ ! -e "$(sealed_store_dir)" ]] || fail "a store was created from an empty pigpen"

  # -pigpen and -encryption are two different key schemes.
  run_madder_agent init -pigpen "$PIGPEN" -encryption generate .sealed
  assert_failure
  assert_output --partial 'cannot be combined with -encryption'
  [[ ! -e "$(sealed_store_dir)" ]] || fail "a store was created despite the conflict"
}

# second_recipient echoes a valid X25519 recipient the test holds no key
# for: the public key of a throwaway sealed-key store.
second_recipient() {
  run_madder_agent init -pigpen "$PIGPEN" .throwaway
  assert_success
  local id
  id="$(grep -o 'piggy-recipient-v1@age_x25519_pub-[a-z0-9]*' \
    .madder/local/share/blob_stores/throwaway/blob_store-config | head -n 1)"
  [[ -n $id ]] || fail "no public key found in the throwaway store's config"
  echo "$id"
}

# pigpen_document echoes the recipients of $PIGPEN as a pigpen recipient-set
# document, which is what a pointer resolver must print. It is lifted from
# the snapshot a path-kind store embeds in its sidecar.
pigpen_document() {
  local name="snapshot-$RANDOM"
  run_madder_agent init -pigpen "$PIGPEN" ".$name"
  assert_success
  awk '/^pigpen = """/ { on = 1; sub(/^pigpen = """/, ""); if ($0 == "") next }
       on && /^"""/ { exit }
       on { print }' ".madder/local/share/blob_stores/$name/blob_store-key"
}

# install_fake_papi_resolver puts a `pigpen-resolver-papi-http` first on
# PATH. It logs each call and prints $FAKE_PIGPEN, or fails as if offline
# when that file is absent.
install_fake_papi_resolver() {
  FAKE_PIGPEN="$BATS_TEST_TMPDIR/published-pigpen"
  RESOLVER_LOG="$BATS_TEST_TMPDIR/resolver.log"
  : >"$RESOLVER_LOG"
  mkdir -p "$BATS_TEST_TMPDIR/bin"
  cat >"$BATS_TEST_TMPDIR/bin/pigpen-resolver-papi-http" <<EOF
#! /bin/sh
echo "\$*" >>"$RESOLVER_LOG"
if [ ! -f "$FAKE_PIGPEN" ]; then
  echo "pigpen: resolve https://\$2/papi/pigpen: fetch failed: offline" >&2
  exit 1
fi
cat "$FAKE_PIGPEN"
EOF
  chmod +x "$BATS_TEST_TMPDIR/bin/pigpen-resolver-papi-http"
  export PATH="$BATS_TEST_TMPDIR/bin:$PATH"
}

resolver_call_count() {
  wc -l <"$RESOLVER_LOG" | tr -d ' '
}

function sidecar_is_plain_text_with_heredocs { # @test
  init_sealed_store

  local sidecar
  sidecar="$(sealed_store_dir)/blob_store-key"

  run cat "$sidecar"
  assert_output --partial '! toml-blob_store_key-v2'
  assert_output --partial 'source_kind = "path"'
  assert_output --partial "source = \"$PIGPEN\""
  assert_output --partial 'pigpen = """'
  assert_output --partial 'document = """'
  refute_output --partial 'digest'

  # The sealed document's ciphertext is base64 on disk: no raw bytes.
  run grep -c -P '[^\x20-\x7e]' "$sidecar"
  assert_output '0'
}

function pigpen_kind_embedded_records_no_path { # @test
  run_madder_agent init -pigpen "$PIGPEN" -pigpen-kind embedded .sealed
  assert_success

  run cat "$(sealed_store_dir)/blob_store-key"
  assert_output --partial 'source_kind = "embedded"'
  refute_output --partial "$PIGPEN"
  refute_output --partial 'source = '

  local blob_id
  blob_id="$(write_sealed_blob "sealed to an embedded pigpen")"

  # The file it was copied from is no longer needed for anything.
  rm "$PIGPEN"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'sealed to an embedded pigpen'
  refute_output --partial 'warning'

  run_madder_no_agent key-status .sealed
  assert_success
  assert_output --partial 'pigpen:      embedded in blob_store-key'
  assert_output --partial 'status:      in sync'
}

function pigpen_kind_papi_resolves_once_then_uses_the_cache { # @test
  install_fake_papi_resolver
  pigpen_document >"$FAKE_PIGPEN"
  [[ -s $FAKE_PIGPEN ]] || fail "could not build a pigpen document"

  run_madder_agent init -pigpen example.test -pigpen-kind papi .sealed
  assert_success
  [[ "$(resolver_call_count)" == "1" ]] || fail "init made $(resolver_call_count) resolver calls"

  run cat "$RESOLVER_LOG"
  assert_output 'resolve example.test'

  # The remote-visible sidecar names the domain and holds the pointer, and
  # nothing about this machine.
  run cat "$(sealed_store_dir)/blob_store-key"
  assert_output --partial 'source_kind = "papi"'
  assert_output --partial 'source = "example.test"'
  assert_output --partial '! pigpen-pointer-v1'
  assert_output --partial 'kind="papi-http"'
  refute_output --partial "$BATS_TEST_TMPDIR"

  local blob_id
  blob_id="$(write_sealed_blob "sealed to a published pigpen")"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'sealed to a published pigpen'
  refute_output --partial 'warning'

  # Ordinary commands compared against the cache: no further fetch.
  [[ "$(resolver_call_count)" == "1" ]] ||
    fail "write and cat made resolver calls: $(resolver_call_count) total"
}

function pigpen_kind_papi_drift_is_seen_after_key_status { # @test
  install_fake_papi_resolver
  pigpen_document >"$FAKE_PIGPEN"

  run_madder_agent init -pigpen example.test -pigpen-kind papi .sealed
  assert_success

  local blob_id added
  blob_id="$(write_sealed_blob "before the published pigpen changed")"
  added="$(second_recipient)"

  # The published pigpen gains a recipient.
  echo "$added" >>"$PIGPEN"
  pigpen_document >"$FAKE_PIGPEN"

  # Until something fetches, the cache still says the old set: no warning,
  # and no network.
  local before
  before="$(resolver_call_count)"
  run_madder_agent cat .sealed "$blob_id"
  assert_success
  refute_output --partial 'recipient set has changed'
  [[ "$(resolver_call_count)" == "$before" ]] || fail "cat fetched the pigpen"

  # key-status fetches afresh and refreshes the cache.
  run_madder_no_agent key-status .sealed
  assert_success
  assert_output --partial 'pigpen:      example.test (papi)'
  assert_output --partial "added:       ${added#*@}"
  assert_output --partial 'status:      changed'
  [[ "$(resolver_call_count)" == "$((before + 1))" ]] || fail "key-status did not fetch"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'recipient set has changed'
  [[ "$(resolver_call_count)" == "$((before + 1))" ]] || fail "cat fetched the pigpen"

  run_madder_agent key-reseal -confirm .sealed
  assert_success
  assert_output --partial 're-sealed .sealed to 2 recipients ('

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'before the published pigpen changed'
  refute_output --partial 'recipient set has changed'
}

function pigpen_kind_papi_offline { # @test
  install_fake_papi_resolver
  pigpen_document >"$FAKE_PIGPEN"

  run_madder_agent init -pigpen example.test -pigpen-kind papi .sealed
  assert_success

  local blob_id
  blob_id="$(write_sealed_blob "read while offline")"

  # Offline with a warm cache: reads are silent and make no request.
  rm "$FAKE_PIGPEN"
  local before
  before="$(resolver_call_count)"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'read while offline'
  refute_output --partial 'warning'
  [[ "$(resolver_call_count)" == "$before" ]] || fail "cat tried to fetch"

  # The commands that must have a live answer fail, and say why.
  run_madder_no_agent key-status .sealed
  assert_failure
  assert_output --partial 'fetch failed: offline'

  run_madder_agent key-reseal .sealed
  assert_failure

  # Offline with a cold cache: a warning, and the read still works.
  rm -rf "$XDG_CACHE_HOME/madder/pigpen"
  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'read while offline'
  assert_output --partial 'cannot re-read the pigpen'
}

function key_reseal_refuses_a_changed_set_without_confirm { # @test
  init_sealed_store

  local sidecar before added
  sidecar="$(sealed_store_dir)/blob_store-key"
  before="$(cat "$sidecar")"
  added="$(second_recipient)"
  echo "$added" >>"$PIGPEN"

  local ecdh_before
  ecdh_before="$(card_ecdh_count)"

  run_madder_agent key-reseal .sealed
  assert_failure
  assert_output --partial "added:       ${added#*@}"
  assert_output --partial 're-run with -confirm'

  [[ "$(cat "$sidecar")" == "$before" ]] || fail "the sidecar was replaced without -confirm"
  [[ "$(card_ecdh_count)" == "$ecdh_before" ]] || fail "the refused reseal used the card"
}

function key_status_reports_in_sync_without_the_card { # @test
  init_sealed_store

  run_madder_no_agent key-status .sealed
  assert_success
  assert_output --partial "pigpen:      $PIGPEN"
  assert_output --partial 'sealed to:   1 recipient ('
  assert_output --partial 'pigpen now:  1 recipient ('
  assert_output --partial 'status:      in sync'
  refute_output --partial 'added:'
  refute_output --partial 'removed:'

  [[ "$(card_ecdh_count)" == "0" ]] || fail "key-status used the card"
}

function drift_warns_and_key_reseal_adds_a_recipient { # @test
  init_sealed_store

  local blob_id added
  blob_id="$(write_sealed_blob "written before the reseal")"
  added="$(second_recipient)"
  echo "$added" >>"$PIGPEN"

  # A changed pigpen warns and proceeds: the blob is still readable.
  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'written before the reseal'
  assert_output --partial 'recipient set has changed'
  assert_output --partial 'madder key-status .sealed'

  run_madder_no_agent key-status .sealed
  assert_success
  assert_output --partial 'sealed to:   1 recipient ('
  assert_output --partial 'pigpen now:  2 recipients ('
  assert_output --partial "added:       ${added#*@}"
  assert_output --partial 'status:      changed'

  local before_reseal
  before_reseal="$(card_ecdh_count)"

  run_madder_agent key-reseal -confirm .sealed
  assert_success
  assert_output --partial 're-sealed .sealed to 2 recipients ('

  # Re-sealing opened the old document once, and read no blob.
  [[ "$(($(card_ecdh_count) - before_reseal))" == "1" ]] ||
    fail "expected 1 card ECDH for the reseal"

  run_madder_no_agent key-status .sealed
  assert_success
  assert_output --partial 'sealed to:   2 recipients ('
  assert_output --partial 'status:      in sync'

  # Same store key: the old blob still decrypts, and the warning is gone.
  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'written before the reseal'
  refute_output --partial 'recipient set has changed'
}

function key_reseal_can_remove_the_card { # @test
  init_sealed_store

  local blob_id other
  blob_id="$(write_sealed_blob "sealed to the card")"
  other="$(second_recipient)"
  echo "$other" >"$PIGPEN"

  run_madder_no_agent key-status .sealed
  assert_success
  # Recipients are listed by key, without the purpose prefix.
  assert_output --partial "added:       ${other#*@}"
  assert_output --partial "removed:     ${PIV_RECIPIENT_ID#*@}"

  run_madder_agent key-reseal -confirm .sealed
  assert_success

  # The card no longer opens the new sidecar. This is removal, not
  # revocation: see FDR 0011.
  run_madder_agent cat .sealed "$blob_id"
  assert_failure
  refute_output --partial 'sealed to the card'
  assert_output --partial '1 blob(s) could not be read'
}

function key_reseal_without_an_agent_changes_nothing { # @test
  init_sealed_store

  local sidecar before
  sidecar="$(sealed_store_dir)/blob_store-key"
  before="$(cat "$sidecar")"

  second_recipient >>"$PIGPEN"

  # -confirm, so that the only thing left to fail is the agent.
  run_madder_no_agent key-reseal -confirm .sealed
  assert_failure
  assert_output --partial 're-sealing the store key'
  [[ "$(cat "$sidecar")" == "$before" ]] || fail "the sidecar was replaced"
}

function a_missing_pigpen_warns_but_does_not_block_reads { # @test
  init_sealed_store

  local blob_id
  blob_id="$(write_sealed_blob "pigpen went away")"
  rm "$PIGPEN"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'pigpen went away'
  assert_output --partial 'cannot re-read the pigpen'

  run_madder_no_agent key-status .sealed
  assert_failure
  assert_output --partial 'sealed to:   1 recipient ('
}

function key_reseal_pigpen_flag_records_a_moved_pigpen { # @test
  init_sealed_store

  local moved="$BATS_TEST_TMPDIR/moved-piggy-ids"
  mv "$PIGPEN" "$moved"

  run_madder_agent key-reseal -pigpen "$moved" .sealed
  assert_success

  run_madder_no_agent key-status .sealed
  assert_success
  assert_output --partial "pigpen:      $moved"
  assert_output --partial 'status:      in sync'
}

function key_commands_refuse_a_store_without_a_sealed_key { # @test
  run_madder init -encryption none .plain
  assert_success

  run_madder_agent key-status .plain
  assert_failure
  assert_output --partial 'is not a sealed-key store'

  run_madder_agent key-reseal .plain
  assert_failure
  assert_output --partial 'is not a sealed-key store'
}

function init_pigpen_does_not_replace_an_existing_stores_key { # @test
  init_sealed_store

  local blob_id
  blob_id="$(write_sealed_blob "written under the first key")"

  local sidecar before
  sidecar="$(sealed_store_dir)/blob_store-key"
  before="$(cat "$sidecar")"

  # A second init must fail and must leave the sidecar alone: replacing
  # it would orphan every blob already written.
  run_madder_agent init -pigpen "$PIGPEN" .sealed
  assert_failure
  [[ "$(cat "$sidecar")" == "$before" ]] || fail "the existing sidecar was replaced"

  run_madder_agent cat .sealed "$blob_id"
  assert_success
  assert_output --partial 'written under the first key'
}
