setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=resolution_conformance

# Conformance suite for FDR-0010 (Scoped-ID Resolution),
# docs/features/0010-scoped-id-resolution.md.
#
# Tests are written against the SPEC, not against today's behaviour, and
# use NON-"default" store names throughout (widgets/gadgets/sprockets) —
# the existing corpus is almost entirely "default", the exact blind spot
# that let spec and impl drift (dodder#359 is a "default" bug).
#
# `madder info-repo <id> config-path` is the id -> physical-location
# probe: it prints the config path the id resolves to, so two resolvers
# disagreeing show up as two different paths.
#
# Scenarios that FAIL against today's implementation are marked
# EXPECTED-FAIL with a bats `skip` naming the Divergence Inventory entry
# they pin. Do NOT change the implementation to make them pass in this
# pass — they flip to real assertions when the single resolver lands.

# --- helpers ---------------------------------------------------------

# two_ancestor_widgets builds outer/inner/leaf, inits a CWD store named
# `widgets` at BOTH outer and inner (same name, two depths), and leaves
# the shell in leaf. Deepest-first: inner is `.widgets`, outer is
# `..widgets`.
two_ancestor_widgets() {
  mkdir -p outer/inner/leaf
  cd outer || exit
  run_madder init -encryption none .widgets
  assert_success
  cd inner || exit
  run_madder init -encryption none .widgets
  assert_success
  cd leaf || exit
}

# --- PASS anchors ----------------------------------------------------

# FDR-0010 walk-up definition: the operate resolver ranks same-named CWD
# ancestors deepest-first, so `.widgets` is the nearest and `..widgets`
# the next. PASS anchor: today's operate path (info-repo) already
# implements the semantics the spec adopts as the ONE resolver.
function operate_resolves_same_named_ancestors_deepest_first { # @test
  two_ancestor_widgets

  run_madder info-repo .widgets config-path
  assert_success
  assert_output --regexp '/outer/inner/\.madder/'

  run_madder info-repo ..widgets config-path
  assert_success
  assert_output --regexp '/outer/\.madder/'
}

# FDR-0010 Error Contract + Ambiguity rules: a CWD dot-depth overflow
# (asking for a 3rd "widgets" when only two exist) MUST fail fast and
# state the matches that DO exist, not silently clamp to the nearest.
# PASS anchor: the operate path already errors with the available list.
function operate_overflow_errors_with_available_list { # @test
  two_ancestor_widgets

  run_madder info-repo ...widgets config-path
  assert_failure
  assert_output --partial '.widgets'
  assert_output --partial '..widgets'
}

# FDR-0010 XDG-user rule + madder#227 (fixed): an unprefixed id is
# home-pinned and MUST NOT resolve to, or be shadowed by, a CWD-ancestor
# store of the same name. PASS anchor / regression guard: `widgets`
# (XDG user) and `.widgets` (CWD ancestor) resolve to DIFFERENT physical
# stores, and the user id never lands in the ancestor `.madder`.
function unprefixed_user_id_is_not_shadowed_by_ancestor { # @test
  mkdir -p outer/leaf
  cd outer || exit
  run_madder init -encryption none .widgets # CWD ancestor store
  assert_success
  cd leaf || exit

  # Unprefixed init must home-pin (XDG user), NOT land in ../.madder.
  run_madder init -encryption none widgets
  assert_success

  run_madder info-repo .widgets config-path
  assert_success
  assert_output --regexp '/outer/\.madder/'
  local cwd_path="$output"

  run_madder info-repo widgets config-path
  assert_success
  # The XDG-user store is a DIFFERENT path, and is not rooted under the
  # ancestor's `.madder/`.
  refute_output --partial '/outer/.madder/'
  refute_output "$cwd_path"
}

# --- EXPECTED-FAIL (documented divergences) --------------------------

# EXPECTED-FAIL — FDR-0010 "The Init Exception" / Divergence #1
# (env_dir.MakeDefaultAndInitialize CWD branch, resolveCwdAncestorOrError).
# Init MUST create at $PWD and MUST reject a positive-depth CWD id: a
# dot-depth is an addressing-only spelling with no existing ancestor to
# select at creation time. Today init walks up `depth` literal parents
# and creates the store THERE (pinned by the Go test
# TestMakeDefaultAndInitialize_MultiDotResolvesAncestors), so `init
# ..gadgets` succeeds at the parent instead of erroring.
function init_rejects_multi_dot_cwd_id { # @test
  skip "EXPECTED-FAIL (FDR-0010 Init Exception / Divergence #1): init must \
reject a positive-depth CWD id and create at \$PWD; today it walks up literal \
parents and creates the store at the ancestor. Unify onto create-at-\$PWD, \
then delete this skip."

  mkdir -p outer/inner/leaf
  cd outer/inner/leaf || exit

  # `..gadgets` (dot-depth 1): addressing-only; init must refuse it.
  run_madder init -encryption none ..gadgets
  assert_failure
  assert_output --partial 'current directory'
  # And no store may have been created at the parent.
  refute [ -d ../.madder/local/share/blob_stores/gadgets ]
}

# EXPECTED-FAIL — FDR-0010 "Legacy Layouts Are Errors" / Divergence #5
# (directory_layout.GetBlobStoreConfigPaths) and madder#175. A legacy
# `dodder-blob_store-config` at a resolved location MUST produce a
# per-id, resolve-time error that NAMES A MIGRATION TOOL. Today the
# error fires at glob time and prescribes a manual "rename each to
# blob_store-config" with no named tool (the former
# `migrate-legacy-configs` command was removed; madder#28). madder#175
# tracks improving this error into a copy-pasteable / auto-rename form.
function legacy_layout_errors_naming_a_migration_tool { # @test
  skip "EXPECTED-FAIL (FDR-0010 Legacy Layouts / Divergence #5; madder#175): \
the legacy-config error must name a migration tool; today it says 'rename each \
to blob_store-config' with no tool (migrate-legacy-configs was removed, \
madder#28). Land the madder#175 recovery, then delete this skip."

  run_madder init -encryption none .sprockets
  assert_success

  local config=".madder/local/share/blob_stores/sprockets/blob_store-config"
  [[ -f $config ]] || fail "expected config at $config"

  # Downgrade the on-disk config to the legacy filename.
  chmod u+w "$(dirname "$config")" "$config" 2>/dev/null || true
  mv -- "$config" "$(dirname "$config")/dodder-blob_store-config"

  run_madder list
  assert_failure
  # Names the offending store id and points at a migration tool, not a
  # bare manual rename.
  assert_output --partial 'sprockets'
  assert_output --regexp 'migrat'
}
