#! /bin/bash -e

# Helpers for sftp-analyze-and-suggest-configs bats tests. Pair with
# lib/sftp.bash (which spawns the test SFTP server and writes the
# host key) and lib/common.bash (run_madder, fail, assert_*).

# start_test_ssh_agent spawns ssh-agent, generates a fresh ed25519
# keypair, loads it into the agent, and exports SSH_AUTH_SOCK. Pair
# with stop_test_ssh_agent in teardown. madder's
# MakeSSHClientFromSSHConfig uses pubkey-via-agent auth.
#
# The agent socket is bound under a fresh /tmp/-rooted dir via
# `ssh-agent -a`. We cannot rely on $TMPDIR for the socket location:
# inside the macOS nix build sandbox bats sits under a long $TMPDIR
# (/nix/var/nix/builds/nix-NNNNN-NNNNNNNNN) and $BATS_TEST_TMPDIR
# inherits that prefix, so the agent's default listener dir
# overruns darwin's 104-byte AF_UNIX sun_path limit. The kernel
# stores the literal path passed to bind(2), so an explicit
# `-a /tmp/.../agent.sock` keeps the bound path short regardless of
# where bats's per-test tree lives. See madder#207.
start_test_ssh_agent() {
  local key="$BATS_TEST_TMPDIR/test_ed25519"

  ssh-keygen -t ed25519 -N '' -f "$key" -q

  local sock_dir
  sock_dir=$(mktemp -d /tmp/mdr.XXXXXX)
  TEST_SSH_AGENT_SOCK_DIR="$sock_dir"

  local agent_output
  agent_output="$(ssh-agent -a "$sock_dir/agent.sock" -s)"
  eval "$agent_output" >/dev/null

  ssh-add "$key" 2>/dev/null

  export SSH_AUTH_SOCK
  export SSH_AGENT_PID
  export TEST_SSH_AGENT_KEY="$key"
  export TEST_SSH_AGENT_SOCK_DIR
}

stop_test_ssh_agent() {
  if [[ -n ${SSH_AGENT_PID:-} ]]; then
    kill "$SSH_AGENT_PID" 2>/dev/null || true
    unset SSH_AGENT_PID
  fi
  if [[ -n ${TEST_SSH_AGENT_SOCK_DIR:-} ]]; then
    rm -rf "$TEST_SSH_AGENT_SOCK_DIR"
    unset TEST_SSH_AGENT_SOCK_DIR
  fi
  unset SSH_AUTH_SOCK TEST_SSH_AGENT_KEY
}

# write_test_ssh_config <alias> <host> <port> <user> <known_hosts>
# Writes an isolated ssh_config under $BATS_TEST_TMPDIR/home/.ssh/
# and exports HOME so madder's ssh_config lookup finds the alias.
# Avoids polluting the user's real ~/.ssh/config.
write_test_ssh_config() {
  local alias="$1" host="$2" port="$3" user="$4" known_hosts="$5"
  local fake_home="$BATS_TEST_TMPDIR/home"
  mkdir -p "$fake_home/.ssh"
  cat >"$fake_home/.ssh/config" <<EOF
Host $alias
  HostName $host
  Port $port
  User $user
  UserKnownHostsFile $known_hosts
  StrictHostKeyChecking yes
  PasswordAuthentication yes
EOF
  chmod 600 "$fake_home/.ssh/config"
  export HOME="$fake_home"
}

# craft_legacy_blob <comp> <enc> <recipient_or_-> <content_path> <out_path>
# Writes a blob with the named compression and (optional) age
# encryption to <out_path>. recipient is an age private-key path
# when enc=age, ignored otherwise.
craft_legacy_blob() {
  local bin="${MADDER_TEST_CRAFT_LEGACY_BLOB:-madder-test-craft-legacy-blob}"
  "$bin" \
    -compression "$1" \
    -encryption "$2" \
    -recipient "$3" \
    -content "$4" \
    -out "$5"
}

# place_legacy_blob_at_correct_path <root> <comp> <enc> <recipient_or_-> <content>
# Hashes <content> with sha256, writes <root>/<HH>/<rest> with bytes
# crafted by craft_legacy_blob, where HH is the first two hex chars
# of the digest and <rest> is the remaining 62 chars. Mirrors the
# default hash-bucketed layout (HashBuckets=[2]) used by the
# existing init flow.
place_legacy_blob_at_correct_path() {
  local root="$1" comp="$2" enc="$3" recip="$4" content="$5"

  local hex
  hex="$(printf "%s" "$content" | sha256sum | awk '{print $1}')"
  local prefix="${hex:0:2}"
  local rest="${hex:2}"
  mkdir -p "$root/$prefix"

  local content_path="$BATS_TEST_TMPDIR/.tmp-content-$$-$RANDOM"
  printf "%s" "$content" >"$content_path"

  craft_legacy_blob "$comp" "$enc" "$recip" "$content_path" "$root/$prefix/$rest"
  rm "$content_path"
}
