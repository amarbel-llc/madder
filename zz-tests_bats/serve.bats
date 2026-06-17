# madder serve: unix-socket HTTP blob API (GET/HEAD/PUT) round-trips.
#
# Promotes the debug-serve-blob-api recipe to a CI lane. Covers the
# ambient backend (serves the discovered cwd .default store) and the
# single-store --store backend, mirroring that recipe's vetted
# behavior. The //default system scope needs /var/lib/madder and is
# covered by the Go tests (command_components), not here.
#
# Deliberately NOT net_cap: the API is an AF_UNIX socket in
# $BATS_TEST_TMPDIR — a filesystem object, not a TCP loopback bind — so
# it needs no local-binding grant and runs in the default lane.

setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output

  SERVE_SOCK="$BATS_TEST_TMPDIR/madder.sock"
  SERVE_PID=""

  init_store .default
}

teardown() {
  serve_stop
}

# serve_start [extra args...] launches `madder serve --socket $SERVE_SOCK`
# in the background — NOT through run_madder, whose 2s timeout would reap
# the daemon — and blocks until the socket appears. The daemon's stderr
# (store-discovery chatter) lands in serve.log, surfaced only on failure.
serve_start() {
  local bin="${MADDER_BIN:-madder}"
  env MADDER_CEILING_DIRECTORIES="$BATS_TEST_TMPDIR" \
    "$bin" serve --socket "$SERVE_SOCK" "$@" \
    >"$BATS_TEST_TMPDIR/serve.log" 2>&1 &
  SERVE_PID=$!

  local _
  for _ in $(seq 1 50); do
    [ -S "$SERVE_SOCK" ] && return 0
    # If the daemon died before binding, stop waiting and surface its log.
    kill -0 "$SERVE_PID" 2>/dev/null || break
    sleep 0.1
  done

  echo "serve socket never appeared (daemon crashed?):" >&2
  cat "$BATS_TEST_TMPDIR/serve.log" >&2
  return 1
}

serve_stop() {
  [ -n "${SERVE_PID:-}" ] || return 0
  kill "$SERVE_PID" 2>/dev/null || true
  wait "$SERVE_PID" 2>/dev/null || true
  SERVE_PID=""
  rm -f "$SERVE_SOCK"
}

# HTTP helpers over the unix socket. *_code echo the status line; get_body
# echoes the response body.
head_code() {
  curl -s -o /dev/null -w '%{http_code}' -I --unix-socket "$SERVE_SOCK" \
    "http://localhost/blobs/$1"
}

get_body() {
  curl -s --unix-socket "$SERVE_SOCK" "http://localhost/blobs/$1"
}

put_code() {
  printf '%s' "$2" | curl -s -o /dev/null -w '%{http_code}' \
    -X PUT --data-binary @- --unix-socket "$SERVE_SOCK" \
    "http://localhost/blobs/$1"
}

# A syntactically valid digest that is absent from every store.
ABSENT_DIGEST="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"

# seed_blob <store> <content> writes content into the store and echoes its
# digest. The blob is stored, so the served (same) store resolves it.
seed_blob() {
  local blob="$BATS_TEST_TMPDIR/seed-$RANDOM.bin"
  printf '%s' "$2" >"$blob"
  write_blob_id "$1" "$blob"
}

function serve_ambient_head_seeded_blob_is_200 { # @test
  local payload="serve ambient HEAD payload"
  local digest
  digest="$(seed_blob .default "$payload")"
  [[ -n $digest ]] || fail "seed write returned empty digest"

  serve_start
  assert_equal "$(head_code "$digest")" "200"
}

function serve_ambient_get_seeded_blob_matches { # @test
  local payload="serve ambient GET payload"
  local digest
  digest="$(seed_blob .default "$payload")"

  serve_start
  assert_equal "$(get_body "$digest")" "$payload"
}

function serve_ambient_head_absent_blob_is_404 { # @test
  serve_start
  assert_equal "$(head_code "$ABSENT_DIGEST")" "404"
}

function serve_ambient_put_then_get_round_trip { # @test
  local payload="serve PUT round-trip payload"
  local digest
  digest="$(seed_blob .default "$payload")"

  serve_start
  assert_equal "$(put_code "$digest" "$payload")" "201"
  assert_equal "$(get_body "$digest")" "$payload"
}

function serve_ambient_put_digest_mismatch_is_409 { # @test
  serve_start
  # Bytes whose real digest is not $ABSENT_DIGEST: the handler must
  # verify content against the claimed digest and reject the mismatch.
  assert_equal "$(put_code "$ABSENT_DIGEST" "unrelated bytes")" "409"
}

function serve_store_flag_serves_single_store_by_id { # @test
  # --store opens exactly one store by id (storeBackend) instead of the
  # ambient search. .default is a writable cwd store; the //default
  # system path needs /var/lib/madder and is covered by the Go tests.
  local payload="serve --store payload"
  local digest
  digest="$(seed_blob .default "$payload")"

  serve_start --store .default
  assert_equal "$(get_body "$digest")" "$payload"
}
