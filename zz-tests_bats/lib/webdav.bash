#! /bin/bash -e

# start_webdav_server spawns madder-test-webdav-server as a coproc per
# RFC 0001. Reads and validates the handshake line, then exports
# WEBDAV_ADDR (host:port), WEBDAV_URL (http://host:port/), and
# WEBDAV_ROOT (the on-disk path the server is vending — useful for
# tests that need to assert on-disk shape after a write). Mirrors
# start_sftp_server in lib/sftp.bash.
#
# Caller contract: must call from a setup() that has already loaded
# common.bash. Pair with stop_webdav_server in teardown().
start_webdav_server() {
  _start_webdav_server_inner "http"
}

# start_webdav_server_tls spawns the test server with -tls. Sets
# WEBDAV_URL to an https:// URL and exports WEBDAV_CERT_PATH so tests
# can pin the CA via -tls-ca-path on init-webdav.
start_webdav_server_tls() {
  _start_webdav_server_inner "https"
}

_start_webdav_server_inner() {
  local scheme="$1"
  require_bin MADDER_TEST_WEBDAV_SERVER madder-test-webdav-server
  local webdav_bin="${MADDER_TEST_WEBDAV_SERVER:-madder-test-webdav-server}"

  local cookie
  cookie="$(head -c 16 /dev/urandom | xxd -p)"

  local stderr_file="$BATS_TEST_TMPDIR/madder-test-webdav-server.stderr"

  local -a server_args=()
  if [[ $scheme == "https" ]]; then
    server_args+=("-tls")
  fi

  coproc WEBDAV_PROC {
    MADDER_PLUGIN_COOKIE="$cookie" \
      "$webdav_bin" "${server_args[@]}" 2>"$stderr_file"
  }
  export WEBDAV_STDOUT_FD="${WEBDAV_PROC[0]}"
  export WEBDAV_STDIN_FD="${WEBDAV_PROC[1]}"
  export WEBDAV_PID="$WEBDAV_PROC_PID"

  local line
  if ! read -r -t 5 -u "$WEBDAV_STDOUT_FD" line; then
    local stderr_contents
    stderr_contents="$(cat "$stderr_file" 2>/dev/null || echo '<no stderr>')"
    fail "WebDAV handshake timeout after 5s. stderr: $stderr_contents"
  fi

  local -a fields
  IFS='|' read -ra fields <<<"$line"
  if [[ ${#fields[@]} -ne 6 ]]; then
    fail "WebDAV handshake malformed (want 6 fields, got ${#fields[@]}): $line"
  fi
  if [[ ${fields[0]} != "$cookie" ]]; then
    fail "WebDAV handshake cookie mismatch: got ${fields[0]}, want $cookie"
  fi
  if [[ ${fields[1]} != "1" ]]; then
    fail "WebDAV handshake version: got ${fields[1]}, want 1"
  fi
  if [[ ${fields[5]} != "$scheme" ]]; then
    fail "WebDAV handshake subprotocol: got ${fields[5]}, want $scheme"
  fi

  export WEBDAV_ADDR="${fields[3]}"
  export WEBDAV_URL="${scheme}://${WEBDAV_ADDR}/"

  # Parse the &-separated metadata pairs into WEBDAV_ROOT and
  # WEBDAV_CERT_PATH. root= is always present; cert= only in TLS mode.
  local metadata="${fields[4]}"
  local kv key value
  unset WEBDAV_ROOT WEBDAV_CERT_PATH
  while IFS= read -r kv; do
    [[ -z $kv ]] && continue
    key="${kv%%=*}"
    value="${kv#*=}"
    case "$key" in
      root) export WEBDAV_ROOT="$value" ;;
      cert) export WEBDAV_CERT_PATH="$value" ;;
    esac
  done < <(printf '%s\n' "${metadata//&/$'\n'}")

  if [[ -z ${WEBDAV_ROOT:-} ]]; then
    fail "WebDAV handshake missing root= metadata: ${fields[4]}"
  fi
  if [[ $scheme == "https" && -z ${WEBDAV_CERT_PATH:-} ]]; then
    fail "WebDAV TLS handshake missing cert= metadata: ${fields[4]}"
  fi
}

# init_webdav_test_store provisions a WebDAV-backed blob store rooted
# at <url>/<remote_subpath>/ on the running test WebDAV server. Caller
# can override the subpath, store id, and any extra init-webdav args.
init_webdav_test_store() {
  local remote_subpath="${1:-store-1}"
  local store_id="${2:-.webdav-test}"
  shift 2 || true

  run_madder init-webdav \
    -url "${WEBDAV_URL}${remote_subpath}/" \
    "$@" \
    "$store_id"
  assert_success
}

# stop_webdav_server closes the child's stdin (RFC 0001 graceful
# shutdown signal), then reaps the child.
stop_webdav_server() {
  if [[ -n ${WEBDAV_STDIN_FD:-} ]]; then
    eval "exec ${WEBDAV_STDIN_FD}>&-"
    unset WEBDAV_STDIN_FD
  fi
  if [[ -n ${WEBDAV_STDOUT_FD:-} ]]; then
    eval "exec ${WEBDAV_STDOUT_FD}<&-"
    unset WEBDAV_STDOUT_FD
  fi
  if [[ -n ${WEBDAV_PID:-} ]]; then
    wait "$WEBDAV_PID" 2>/dev/null || true
    unset WEBDAV_PID
  fi
  unset WEBDAV_ADDR WEBDAV_URL WEBDAV_ROOT WEBDAV_CERT_PATH
}
