setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=capture_tree

# extract the receipt id of the first (or only) store group from a
# capture-tree NDJSON stream.
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

function capture_tree_simple_dir { # @test

  init_store

  mkdir -p tree/sub
  echo "alpha" >tree/a.txt
  echo "beta" >tree/b.txt
  echo "gamma" >tree/sub/c.txt

  run_madder capture-tree -format json tree
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

function capture_tree_records_symlink_target { # @test

  init_store

  mkdir -p tree
  echo "real" >tree/real.txt
  ln -s real.txt tree/link.txt

  run_madder capture-tree -format json tree
  assert_success

  local rid
  rid="$(receipt_id_of_group "$output")"
  [[ -n $rid ]] || fail "no receipt id: $output"

  run_madder cat "$rid"
  assert_success

  echo "$output" | grep -q '"path":"link.txt","root":"tree","type":"symlink".*"target":"real.txt"' ||
    fail "symlink entry missing or wrong shape: $output"
}

function capture_tree_includes_dotfiles { # @test

  init_store

  mkdir -p tree
  echo "v" >tree/.hidden
  echo "x" >tree/visible

  run_madder capture-tree -format json tree
  assert_success

  local rid
  rid="$(receipt_id_of_group "$output")"
  run_madder cat "$rid"
  assert_success

  echo "$output" | grep -q '"path":".hidden"' ||
    fail "dotfile not captured: $output"
}

function capture_tree_zero_args_uses_pwd_into_default { # @test

  # Use a non-CWD-relative store so it remains findable after cd.
  run_madder init -encryption none default
  assert_success

  mkdir tree
  echo "x" >tree/x.txt

  cd tree
  run_madder capture-tree -format json
  assert_success

  local rid count store
  rid="$(receipt_id_of_group "$output")"
  count="$(receipt_count_of_group "$output")"
  store="$(receipt_store_of_group "$output")"

  [[ -n $rid ]] || fail "no receipt id: $output"
  [[ $count -eq 2 ]] || fail "expected count=2 (. + x.txt), got $count"
  [[ -z $store ]] || fail "default store should produce empty store id, got '$store'"
}

function capture_tree_one_arg_store_id_uses_pwd { # @test

  # Non-CWD-relative stores so they remain findable after cd.
  run_madder init -encryption none default
  assert_success
  run_madder init -encryption none alt
  assert_success

  mkdir tree
  echo "y" >tree/y.txt

  cd tree
  run_madder capture-tree -format json alt
  assert_success

  local rid count store
  rid="$(receipt_id_of_group "$output")"
  count="$(receipt_count_of_group "$output")"
  store="$(receipt_store_of_group "$output")"

  [[ -n $rid ]] || fail "no receipt id: $output"
  [[ $count -eq 2 ]] || fail "expected count=2 (. + y.txt), got $count"
  [[ $store == "alt" ]] || fail "expected store=alt, got '$store'"
}

function capture_tree_multi_store_group { # @test

  init_store
  run_madder init -encryption none .alt
  assert_success

  mkdir -p src docs
  echo "s" >src/s.txt
  echo "d" >docs/d.txt

  run_madder capture-tree -format json .default src .alt docs
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

function capture_tree_trailing_store_with_no_dirs_errors { # @test

  init_store
  run_madder init -encryption none .alt
  assert_success

  mkdir src
  echo "s" >src/s.txt

  run_madder capture-tree -format json .default src .alt
  assert_failure
}

function capture_tree_back_to_back_stores_errors { # @test

  init_store
  run_madder init -encryption none .alt
  assert_success

  mkdir src
  echo "s" >src/s.txt

  run_madder capture-tree -format json .default .alt src
  assert_failure
}

function capture_tree_is_deterministic { # @test

  init_store

  mkdir -p tree/sub
  echo "alpha" >tree/a.txt
  echo "beta" >tree/b.txt
  echo "gamma" >tree/sub/c.txt

  run_madder capture-tree -format json tree
  assert_success
  local rid_first
  rid_first="$(receipt_id_of_group "$output")"
  [[ -n $rid_first ]] || fail "first run: no receipt id: $output"

  run_madder capture-tree -format json tree
  assert_success
  local rid_second
  rid_second="$(receipt_id_of_group "$output")"
  [[ -n $rid_second ]] || fail "second run: no receipt id: $output"

  [[ $rid_first == "$rid_second" ]] ||
    fail "receipt IDs differ across runs of identical trees: '$rid_first' vs '$rid_second'"
}

function capture_tree_file_arg_is_failure { # @test

  init_store

  echo "lone" >loose.txt

  # capture-tree only takes directories, never files.
  run_madder capture-tree -format json loose.txt
  assert_failure
}
