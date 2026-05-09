setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=issue_145

# Build a tmp tree with two ancestor `.madder/` directories: outer at
# `$BATS_TEST_TMPDIR/outer` and inner at `$BATS_TEST_TMPDIR/outer/inner`.
# Both are below the default ceiling ($BATS_TEST_TMPDIR), so dewey's
# walk-up visits both. CWD is the leaf below the inner ancestor.
multi_ancestor_setup() {
  mkdir -p outer/inner/leaf
  cd outer
  init_store .outer_only
  init_store .default
  cd inner
  init_store .default
  init_store .inner_only
  cd leaf
}

function list_shows_both_ancestor_stores { # @test
  multi_ancestor_setup
  run_madder list
  assert_success
  assert_output --partial '.outer_only'
  assert_output --partial '.inner_only'
}

function list_disambiguates_collisions_with_extra_dot { # @test
  multi_ancestor_setup
  run_madder list
  assert_success
  # `default` exists at both ancestors; deepest-first (inner) gets the
  # single dot, outer gets the double dot.
  assert_output --partial '.default'
  assert_output --partial '..default'
}

function unique_names_render_with_single_dot { # @test
  multi_ancestor_setup
  run_madder list
  assert_success
  # `outer_only` exists only at the outer ancestor — minimal
  # disambiguation rule says single dot, NOT `..outer_only`.
  refute_line --regexp '^\.\.\.outer_only:'
  refute_line --regexp '^\.\.outer_only:'
  assert_output --partial '.outer_only'
}

function info_repo_resolves_outer_via_double_dot { # @test
  multi_ancestor_setup
  run_madder info-repo ..default config-path
  assert_success
  assert_output --regexp '/outer/\.madder/'
}

function info_repo_resolves_inner_via_single_dot { # @test
  multi_ancestor_setup
  run_madder info-repo .default config-path
  assert_success
  assert_output --regexp '/outer/inner/\.madder/'
}

function info_repo_unknown_dot_count_lists_available { # @test
  multi_ancestor_setup
  # Three dots — only two ancestors have `default`. Resolution must
  # fail with the disambiguated "available" list.
  run_madder info-repo ...default config-path
  assert_failure
  assert_output --partial '.default'
  assert_output --partial '..default'
}

function dir_blob_stores_reflects_resolved_ancestor { # @test
  # Pre-existing latent bug: `info-repo X dir-blob_stores` printed
  # the env's default path regardless of X. Same fix as config-path.
  multi_ancestor_setup
  run_madder info-repo .default dir-blob_stores
  assert_success
  assert_output --regexp '/outer/inner/\.madder/.*/blob_stores$'

  run_madder info-repo ..default dir-blob_stores
  assert_success
  assert_output --regexp '/outer/\.madder/.*/blob_stores$'
}
