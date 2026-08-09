setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=registry

# The per-host registry index lives under $XDG_STATE_HOME, which
# setup_test_home (bats-island) redirects into $BATS_TEST_TMPDIR — so every
# lane gets an isolated index for free, exactly the sandbox-composition the
# dodder RFC-0007 addendum relies on. No walk-up, no ceiling.
index_dir() { echo "$XDG_STATE_HOME/madder/index"; }

# index_link_count echoes the number of symlink entries in the index dir
# (0 when the dir does not exist yet).
index_link_count() {
  local d
  d="$(index_dir)"
  [[ -d $d ]] || {
    echo 0
    return
  }
  find "$d" -maxdepth 1 -type l | wc -l | tr -d ' '
}

# index_only_link echoes the path of the sole index symlink (fails the test
# if there is not exactly one).
index_only_link() {
  local links
  links="$(find "$(index_dir)" -maxdepth 1 -type l)"
  [[ $(echo "$links" | wc -l) -eq 1 ]] || fail "expected exactly one index link, got: $links"
  echo "$links"
}

function init_registers_cwd_store_as_symlink { # @test
  init_store .default

  assert_equal "$(index_link_count)" 1

  local link target base
  link="$(index_only_link)"
  # The entry is a symlink whose target is the store's blob_store-config.
  target="$(readlink "$link")"
  assert_equal "$(basename "$target")" "blob_store-config"
  # Key is 16 lowercase hex chars, no extension: sha256(base-dir)[:8].
  base="$(basename "$link")"
  [[ $base =~ ^[0-9a-f]{16}$ ]] || fail "index key not 16 hex chars: $base"
}

function init_registers_xdg_user_store_too { # @test
  # All scopes register uniformly (RFC-0007). An unprefixed id is XDG-user.
  init_store default

  assert_equal "$(index_link_count)" 1
  run_madder list -all -format=ndjson
  assert_success
  assert_output --partial '"name":"default"'
}

function list_all_shows_registered_store { # @test
  init_store .default

  run_madder list -all -format=ndjson
  assert_success
  assert_output --partial '"name":".default"'
  assert_output --partial '"type":"local"'
  # A live store is not stale.
  refute_output --partial '"stale":true'
}

function list_all_marks_deleted_store_stale { # @test
  init_store .default
  # Delete the store out from under the index; its symlink now dangles.
  rm -rf .madder

  run_madder list -all -format=ndjson
  assert_success
  assert_output --partial '"stale":true'
  # The inferred scoped name survives from the dead target path.
  assert_output --partial '"name":".default"'
}

function list_all_without_flag_is_unchanged { # @test
  # -all is additive: the plain listing keeps today's columns/behavior.
  init_store .default
  run_madder list -format=tap
  assert_success
  assert_output --partial '.default@blake2b256-'
}

function registry_gc_keeps_fresh_prunes_aged { # @test
  init_store .default
  rm -rf .madder
  assert_equal "$(index_link_count)" 1

  # A just-registered dangling entry is within the grace window — kept.
  run_madder registry-gc -retention=720h
  assert_success
  assert_output --partial 'pruned 0'
  assert_equal "$(index_link_count)" 1

  # Age the dangling symlink's own mtime past a short retention (touch -h
  # sets the link, not its missing target), then prune.
  local link
  link="$(index_only_link)"
  touch -h -d '25 hours ago' "$link"

  run_madder registry-gc -retention=1h
  assert_success
  assert_output --partial 'pruned 1'
  assert_equal "$(index_link_count)" 0
}

function registry_gc_zero_retention_is_noop { # @test
  init_store .default
  rm -rf .madder

  run_madder registry-gc -retention=0
  assert_success
  assert_output --partial 'pruned 0'
  assert_equal "$(index_link_count)" 1
}
