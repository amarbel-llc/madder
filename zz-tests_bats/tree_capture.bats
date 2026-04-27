setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=tree_capture

# extract the receipt id of the first (or only) store group from a
# tree-capture NDJSON stream.
receipt_id_of_group() {
  local out="$1" idx="${2:-1}"
  echo "$out" | grep -F '"type":"store_group_receipt"' |
    sed -n "${idx}p" |
    sed -E 's/.*"receipt_id":"([^"]+)".*/\1/'
}

# extract the count for the first (or only) store group's receipt.
receipt_count_of_group() {
  local out="$1" idx="${2:-1}"
  echo "$out" | grep -F '"type":"store_group_receipt"' |
    sed -n "${idx}p" |
    sed -E 's/.*"count":([0-9]+).*/\1/'
}

# extract the store id of the first (or only) store group's receipt.
receipt_store_of_group() {
  local out="$1" idx="${2:-1}"
  echo "$out" | grep -F '"type":"store_group_receipt"' |
    sed -n "${idx}p" |
    sed -E 's/.*"store":"([^"]*)".*/\1/'
}

function tree_capture_simple_dir { # @test

  init_store

  mkdir -p tree/sub
  echo "alpha" >tree/a.txt
  echo "beta" >tree/b.txt
  echo "gamma" >tree/sub/c.txt

  run_madder tree-capture -format json tree
  assert_success

  # One receipt summary record.
  local n
  n="$(echo "$output" | grep -c '"type":"store_group_receipt"' || true)"
  [[ $n -eq 1 ]] || fail "expected 1 receipt summary, got $n. output:"$'\n'"$output"

  # Three files + tree/ + tree/sub = 5 entries.
  local count
  count="$(receipt_count_of_group "$output")"
  [[ $count -eq 5 ]] || fail "expected count=5 in summary, got $count. output:"$'\n'"$output"

  # Receipt blob is retrievable.
  local rid
  rid="$(receipt_id_of_group "$output")"
  [[ -n $rid ]] || fail "no receipt_id in output: $output"

  run_madder cat "$rid"
  assert_success

  # Hyphence header present.
  echo "$output" | grep -q '^! madder-tree_capture-receipt-v1$' ||
    fail "receipt missing type tag. body: $output"

  # Each captured filename appears as a path field.
  echo "$output" | grep -q '"path":"a.txt"' || fail "missing a.txt: $output"
  echo "$output" | grep -q '"path":"b.txt"' || fail "missing b.txt: $output"
  echo "$output" | grep -q '"path":"sub/c.txt"' || fail "missing sub/c.txt: $output"
  echo "$output" | grep -q '"path":"sub","root":"tree","type":"dir"' ||
    fail "missing sub dir entry: $output"
  echo "$output" | grep -q '"path":".","root":"tree","type":"dir"' ||
    fail "missing root dir entry: $output"
}

function tree_capture_records_symlink_target { # @test

  init_store

  mkdir -p tree
  echo "real" >tree/real.txt
  ln -s real.txt tree/link.txt

  run_madder tree-capture -format json tree
  assert_success

  local rid
  rid="$(receipt_id_of_group "$output")"
  [[ -n $rid ]] || fail "no receipt id: $output"

  run_madder cat "$rid"
  assert_success

  echo "$output" | grep -q '"path":"link.txt","root":"tree","type":"symlink".*"target":"real.txt"' ||
    fail "symlink entry missing or wrong shape: $output"
}

function tree_capture_includes_dotfiles { # @test

  init_store

  mkdir -p tree
  echo "v" >tree/.hidden
  echo "x" >tree/visible

  run_madder tree-capture -format json tree
  assert_success

  local rid
  rid="$(receipt_id_of_group "$output")"
  run_madder cat "$rid"
  assert_success

  echo "$output" | grep -q '"path":".hidden"' ||
    fail "dotfile not captured: $output"
}

function tree_capture_zero_args_uses_pwd_into_default { # @test

  # Use a non-CWD-relative store so it remains findable after cd.
  run_madder init -encryption none default
  assert_success

  mkdir tree
  echo "x" >tree/x.txt

  cd tree
  run_madder tree-capture -format json
  assert_success

  local rid count store
  rid="$(receipt_id_of_group "$output")"
  count="$(receipt_count_of_group "$output")"
  store="$(receipt_store_of_group "$output")"

  [[ -n $rid ]] || fail "no receipt id: $output"
  [[ $count -eq 2 ]] || fail "expected count=2 (. + x.txt), got $count"
  [[ -z $store ]] || fail "default store should produce empty store id, got '$store'"
}

function tree_capture_one_arg_store_id_uses_pwd { # @test

  # Non-CWD-relative stores so they remain findable after cd.
  run_madder init -encryption none default
  assert_success
  run_madder init -encryption none alt
  assert_success

  mkdir tree
  echo "y" >tree/y.txt

  cd tree
  run_madder tree-capture -format json alt
  assert_success

  local rid count store
  rid="$(receipt_id_of_group "$output")"
  count="$(receipt_count_of_group "$output")"
  store="$(receipt_store_of_group "$output")"

  [[ -n $rid ]] || fail "no receipt id: $output"
  [[ $count -eq 2 ]] || fail "expected count=2 (. + y.txt), got $count"
  [[ $store == "alt" ]] || fail "expected store=alt, got '$store'"
}

function tree_capture_multi_store_group { # @test

  init_store
  run_madder init -encryption none .alt
  assert_success

  mkdir -p src docs
  echo "s" >src/s.txt
  echo "d" >docs/d.txt

  run_madder tree-capture -format json .default src .alt docs
  assert_success

  # Two distinct summaries.
  local n
  n="$(echo "$output" | grep -c '"type":"store_group_receipt"' || true)"
  [[ $n -eq 2 ]] || fail "expected 2 receipt summaries, got $n. output:"$'\n'"$output"

  local rid1 store1 rid2 store2
  rid1="$(receipt_id_of_group "$output" 1)"
  store1="$(receipt_store_of_group "$output" 1)"
  rid2="$(receipt_id_of_group "$output" 2)"
  store2="$(receipt_store_of_group "$output" 2)"

  [[ $store1 == ".default" ]] || fail "first group store mismatch: '$store1'"
  [[ $store2 == ".alt" ]] || fail "second group store mismatch: '$store2'"
  [[ $rid1 != "$rid2" ]] || fail "distinct trees should have distinct receipts ($rid1)"
}

function tree_capture_trailing_store_with_no_dirs_errors { # @test

  init_store
  run_madder init -encryption none .alt
  assert_success

  mkdir src
  echo "s" >src/s.txt

  run_madder tree-capture -format json .default src .alt
  assert_failure
}

function tree_capture_back_to_back_stores_errors { # @test

  init_store
  run_madder init -encryption none .alt
  assert_success

  mkdir src
  echo "s" >src/s.txt

  run_madder tree-capture -format json .default .alt src
  assert_failure
}

function tree_capture_is_deterministic { # @test

  init_store

  mkdir -p tree/sub
  echo "alpha" >tree/a.txt
  echo "beta" >tree/b.txt
  echo "gamma" >tree/sub/c.txt

  run_madder tree-capture -format json tree
  assert_success
  local rid_first
  rid_first="$(receipt_id_of_group "$output")"
  [[ -n $rid_first ]] || fail "first run: no receipt id: $output"

  run_madder tree-capture -format json tree
  assert_success
  local rid_second
  rid_second="$(receipt_id_of_group "$output")"
  [[ -n $rid_second ]] || fail "second run: no receipt id: $output"

  [[ $rid_first == "$rid_second" ]] ||
    fail "receipt IDs differ across runs of identical trees: '$rid_first' vs '$rid_second'"
}

function tree_capture_file_arg_is_failure { # @test

  init_store

  echo "lone" >loose.txt

  # tree-capture only takes directories, never files.
  run_madder tree-capture -format json loose.txt
  assert_failure
}

function tree_capture_refuses_parent_escape_root { # @test
  # RFC 0003 §Producer Rules §Root Scoping: capture-roots MUST be PWD
  # or descendants thereof. A `..` from a non-CWD PWD escapes by
  # construction, regardless of what lives above.
  init_store

  mkdir -p inner
  echo "x" >inner/x.txt

  pushd inner >/dev/null
  run_madder tree-capture -format json ..
  popd >/dev/null

  assert_failure
  echo "$output" | grep -qF 'outside working directory' ||
    fail "expected parent-escape refusal: $output"
}

function tree_capture_refuses_absolute_root { # @test
  # RFC 0003 §Producer Rules §Root Scoping: an absolute path that
  # resolves outside PWD MUST be refused. Use a sibling of PWD inside
  # BATS_RUN_TMPDIR so the path exists but is not a descendant of PWD.
  init_store

  local outside="$BATS_RUN_TMPDIR/outside-$$"
  mkdir -p "$outside"
  echo "x" >"$outside/x.txt"

  run_madder tree-capture -format json "$outside"
  assert_failure
  echo "$output" | grep -qF 'outside working directory' ||
    fail "expected absolute-path refusal: $output"
}

function tree_capture_refuses_collision_after_clean { # @test
  # RFC 0003 §Producer Rules §Root Collision Detection: two roots
  # within the same store-group that resolve to the same path under
  # filepath.Clean MUST be refused.
  init_store

  mkdir src
  echo "s" >src/s.txt

  run_madder tree-capture -format json src ./src
  assert_failure
  echo "$output" | grep -qF 'roots "src" and "./src" both resolve to "src"' ||
    fail "expected exact RFC 0003 collision diagnostic: $output"
}

function tree_capture_emits_store_hint_when_known { # @test
  # Per RFC 0003 §Producer Rules §Receipt Metadata: Store Hint, a
  # tree-capture receipt SHOULD carry a `- store/<id> < <markl-id>`
  # line naming the destination store and locking the lookup to that
  # store's blob_store-config blob.
  init_store
  run_madder init -encryption none .work
  assert_success

  mkdir src
  echo "x" >src/x.txt

  run_madder tree-capture -format json .work src
  assert_success

  local rid
  rid="$(receipt_id_of_group "$output")"
  [[ -n $rid ]] || fail "no receipt id: $output"

  run_madder cat .work "$rid"
  assert_success
  echo "$output" | grep -qE '^- store/\.work < blake2b256-' ||
    fail "expected RFC 0003 store-hint line in receipt: $output"
}

function tree_capture_default_store_omits_hint { # @test
  # Per #92 open call (a): the default-store case skips hint emission
  # because the storeID is empty and a synthetic id risks colliding
  # with a user-named store. Confirm no `- store/` line appears.
  init_store

  mkdir src
  echo "x" >src/x.txt

  run_madder tree-capture -format json src
  assert_success

  local rid
  rid="$(receipt_id_of_group "$output")"
  [[ -n $rid ]] || fail "no receipt id: $output"

  run_madder cat "$rid"
  assert_success
  echo "$output" | grep -qE '^- store/' &&
    fail "default-store receipt should not emit a store-hint: $output"
  return 0
}

function tree_capture_warns_when_dir_shadows_store { # @test

  # A bare arg "shadowed" matches both a directory in CWD and a
  # configured blob-store-id. The dir wins (matching `write`'s
  # precedent) and a shadow warning routes to stderr. Capture still
  # succeeds.
  init_store
  run_madder init -encryption none shadowed
  assert_success

  mkdir shadowed
  echo "x" >shadowed/x.txt

  run_madder tree-capture -format json shadowed
  assert_success

  # Receipt is for the default store (the dir won; no store switch).
  local rid store
  rid="$(receipt_id_of_group "$output")"
  store="$(receipt_store_of_group "$output")"

  [[ -n $rid ]] || fail "no receipt id: $output"
  [[ -z $store ]] || fail "expected default store (empty), got '$store'"

  # Shadow warning surfaces somewhere in the run output. NDJSON sink
  # routes notices to stderr; bats merges stdout+stderr into $output.
  echo "$output" | grep -qF 'shadows blob-store-id' ||
    fail "expected shadow warning in output: $output"
}

function tree_capture_per_entry_failure_continues_walk { # @test

  # If one file in the tree is unreadable, the run reports that as a
  # per-entry failure but still captures siblings and exits non-zero.
  # Skip if running as root (chmod 000 doesn't deny root reads).
  if [[ $(id -u) -eq 0 ]]; then
    skip "running as root; chmod 000 has no effect"
  fi

  init_store

  mkdir -p tree
  echo "good" >tree/good.txt
  echo "secret" >tree/secret.txt
  chmod 000 tree/secret.txt

  run_madder tree-capture -format json tree
  # Restore perms before any assert_* might exit, so bats can clean up.
  chmod 644 tree/secret.txt

  assert_failure

  # The good file is captured.
  echo "$output" | grep -q '"path":"good.txt".*"type":"file"' ||
    fail "good.txt should have been captured: $output"

  # An error record names secret.txt.
  echo "$output" | grep -q '"source":"tree/secret.txt".*"error"' ||
    fail "expected error record for secret.txt: $output"
}
