# Two-stage idempotent init: `config-gen` emits a digest-pinned
# blob_store-config artifact; `init-from <id>@<digest>` binds it to a store
# idempotently and drift-detecting. Uses a cwd (`.name`) store under
# $BATS_TEST_TMPDIR — pins are scope-independent, so no /var/lib/madder is
# needed and this stays out of the net_cap lane.
#
# Note: flags precede the positional id throughout (the repo convention; see
# init_store in lib/common.bash) — the flag parser stops at the first
# positional.

setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# gen_config writes the artifact to stdout and its digest to stderr, leaving
# both in $BATS_TEST_TMPDIR. Echoes the digest. Bypasses run_madder so the
# two streams can be redirected separately.
gen_config() {
  local bin="${MADDER_BIN:-madder}"
  "$bin" config-gen -encryption none \
    >"$BATS_TEST_TMPDIR/cfg" 2>"$BATS_TEST_TMPDIR/cfg.digest"
  tail -n1 "$BATS_TEST_TMPDIR/cfg.digest" | tr -d '[:space:]'
}

function config_gen_emits_config_and_digest { # @test
  local digest
  digest="$(gen_config)"
  [[ -n $digest ]] || fail "config-gen printed no digest on stderr"
  # The stdout artifact is a hyphence config carrying its @digest line.
  [[ -s "$BATS_TEST_TMPDIR/cfg" ]] || fail "config-gen wrote an empty artifact"
  grep -q '@' "$BATS_TEST_TMPDIR/cfg" || fail "artifact has no @digest metadata line"
}

function init_from_pinned_installs_then_is_idempotent { # @test
  local digest
  digest="$(gen_config)"

  # First install creates the store.
  run_madder init-from ".pinned@$digest" "$BATS_TEST_TMPDIR/cfg"
  assert_success

  # Re-run with the same pinned id + artifact is an idempotent no-op.
  run_madder init-from ".pinned@$digest" "$BATS_TEST_TMPDIR/cfg"
  assert_success
}

function init_from_pinned_rejects_wrong_pin { # @test
  gen_config >/dev/null

  # A syntactically valid but wrong digest must not match the artifact.
  local wrong="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"
  run_madder init-from ".mismatch@$wrong" "$BATS_TEST_TMPDIR/cfg"
  assert_failure
}

function init_if_not_exists_is_idempotent { # @test
  # Flags precede the positional id (the repo convention; see init_store).
  run_madder init -encryption none --if-not-exists .default
  assert_success
  # Second run must be a no-op success, not the "already exists" error.
  run_madder init -encryption none --if-not-exists .default
  assert_success
}

function init_rejects_pinned_id { # @test
  # `init` (build-from-flags) can't honor a pin; it directs to init-from.
  local wrong="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"
  run_madder init -encryption none ".pinnedinit@$wrong"
  assert_failure
  assert_output --partial 'init-from'
}
