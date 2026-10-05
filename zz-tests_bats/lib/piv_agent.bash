#! /bin/bash -e

# Helpers for running madder against the REAL Rust piggy-agent, backed by
# fibby (piggy's virtual PIV card, which speaks the pcsc-lite daemon
# protocol itself — no pcscd, USB, or privileges). The spawn recipe is
# copied from piggy's own lanes (zz-tests_bats/lib/fibby.bash and
# conformance/piggy_agent_concurrent_sign_fibby.bats); piggy does not
# package them as a library.

# piv_wait_for_socket polls for a unix socket to appear, up to ~5s.
piv_wait_for_socket() {
  local sock="$1" i
  for ((i = 0; i < 50; i++)); do
    [[ -S $sock ]] && return 0
    sleep 0.1
  done
  return 1
}

# start_piv_agent spawns fibby with a seeded slot-9D P-256 key and a
# piggy-agent serving it, then exports:
#   PIVY_AUTH_SOCK    the agent socket (the variable madder's pivy path reads)
#   PIV_FIBBY_SOCK    fibby's pcsc socket, for `piggy list`
#   PIV_FIBBY_LOG     fibby's APDU wire log
#   PIV_RECIPIENT_ID  the slot-9D key as a pivy_ecdh_p256_pub markl id
#
# Pair with stop_piv_agent in teardown().
start_piv_agent() {
  require_bin PIGGY_BIN piggy
  require_bin FIBBY_BIN fibby

  # Not $BATS_TEST_TMPDIR: AF_UNIX sun_path is ~108 bytes and the nix
  # build prefix overruns it.
  PIV_WORKDIR="$(mktemp -d /tmp/piv.XXXXXX)"
  export PIV_FIBBY_SOCK="$PIV_WORKDIR/pcscd.comm"
  export PIV_FIBBY_LOG="$PIV_WORKDIR/fibby.log"
  local agent_sock="$PIV_WORKDIR/a.sock"

  # The agent asks for the PIN on demand through SSH_ASKPASS. This must
  # never prompt or touch /dev/tty. Absolute shebang: /usr/bin/env does
  # not resolve inside the nix sandbox. 123456 is the virtual card's PIN.
  local askpass="$PIV_WORKDIR/askpass"
  printf '#!%s\necho 123456\n' "$BASH" >"$askpass"
  chmod +x "$askpass"

  # 3>&-: close bats' fd 3 on the children, or bats waits on them.
  # --seed-rfc5903-slot-9d-cert installs the key plus a cert and CHUID; a
  # card with no CHUID is not enumerable. FIBBY_LOG=wire logs each APDU.
  FIBBY_LOG=wire "$FIBBY_BIN" \
    --socket "$PIV_FIBBY_SOCK" \
    --backend virtual \
    --seed-rfc5903-slot-9d-cert \
    >"$PIV_FIBBY_LOG" 2>&1 3>&- &
  PIV_FIBBY_PID=$!

  piv_wait_for_socket "$PIV_FIBBY_SOCK" ||
    fail "fibby socket never appeared. log: $(cat "$PIV_FIBBY_LOG")"

  PCSCLITE_CSOCK_NAME="$PIV_FIBBY_SOCK" \
    SSH_ASKPASS="$askpass" \
    SSH_ASKPASS_REQUIRE=force \
    DISPLAY="" \
    "$PIGGY_BIN" agent -A -a "$agent_sock" \
    >"$PIV_WORKDIR/agent.log" 2>&1 3>&- &
  PIV_AGENT_PID=$!

  piv_wait_for_socket "$agent_sock" ||
    fail "piggy-agent socket never appeared. log: $(cat "$PIV_WORKDIR/agent.log")"

  export PIVY_AUTH_SOCK="$agent_sock"

  local listing
  listing="$(PCSCLITE_CSOCK_NAME="$PIV_FIBBY_SOCK" "$PIGGY_BIN" list --format=ndjson 2>&1)" ||
    fail "piggy list failed: $listing"

  PIV_RECIPIENT_ID="$(grep -o 'pivy_ecdh_p256_pub-[a-z0-9]*' <<<"$listing" | head -n 1)"
  [[ -n $PIV_RECIPIENT_ID ]] ||
    fail "no pivy_ecdh_p256_pub id in piggy list output: $listing"
  export PIV_RECIPIENT_ID
}

stop_piv_agent() {
  [[ -n ${PIV_AGENT_PID:-} ]] && kill "$PIV_AGENT_PID" 2>/dev/null
  [[ -n ${PIV_FIBBY_PID:-} ]] && kill "$PIV_FIBBY_PID" 2>/dev/null
  [[ -n ${PIV_AGENT_PID:-} ]] && wait "$PIV_AGENT_PID" 2>/dev/null
  [[ -n ${PIV_FIBBY_PID:-} ]] && wait "$PIV_FIBBY_PID" 2>/dev/null
  [[ -n ${PIV_WORKDIR:-} ]] && rm -rf "$PIV_WORKDIR"
  return 0
}
