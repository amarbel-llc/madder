setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=mcp

function tools_list { # @test
  local bin="${MADDER_BIN:-madder}"
  run bash -c 'printf "%s\n" \
    '\''{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}'\'' \
    '\''{"jsonrpc":"2.0","method":"notifications/initialized"}'\'' \
    '\''{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'\'' \
    | timeout 5s '"$bin"' mcp'

  assert_success
  assert_output --partial "madder_list"
  assert_output --partial "madder_cat"
  assert_output --partial "madder_cat_ids"
  assert_output --partial "madder_info_repo"
  assert_output --partial "madder_fsck"
  assert_output --partial "madder_write"
  assert_output --partial "madder_read"
  assert_output --partial "madder_sync"
  assert_output --partial "madder_init"
  assert_output --partial "madder_init_from"
  assert_output --partial "madder_init_inventory_archive"
  assert_output --partial "madder_init_pointer"
  assert_output --partial "madder_pack"
}

function initialize_response { # @test
  local bin="${MADDER_BIN:-madder}"
  run bash -c 'printf "%s\n" \
    '\''{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}'\'' \
    | timeout 5s '"$bin"' mcp'

  assert_success
  assert_output --partial '"serverInfo"'
  assert_output --partial '"madder"'
  assert_output --partial '"0.1.0"'
}
