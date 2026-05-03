setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output

  HYPHENCE_BIN="$(dirname "${MADDER_BIN:-madder}")/hyphence"
  if [[ ! -x $HYPHENCE_BIN ]]; then
    skip "hyphence binary not found at $HYPHENCE_BIN"
  fi
}

# bats file_tags=hyphence

# run_hyphence wraps `run timeout` the same way run_madder /
# run_cg do in lib/common.bash, so individual tests don't have to
# spell out the binary path or the 2s deadline.
run_hyphence() {
  run timeout --preserve-status 2s "$HYPHENCE_BIN" "$@"
}

# --- validate ---

function validate_accepts_valid_document { # @test
  local f="$BATS_TEST_TMPDIR/valid.hyphence"
  printf -- '---\n! md\n---\n\nhello\n' >"$f"
  run_hyphence validate "$f"
  assert_success
  assert_output ''
}

function validate_accepts_no_body { # @test
  local f="$BATS_TEST_TMPDIR/no-body.hyphence"
  printf -- '---\n! md\n---\n' >"$f"
  run_hyphence validate "$f"
  assert_success
  assert_output ''
}

function validate_rejects_invalid_prefix { # @test
  local f="$BATS_TEST_TMPDIR/bad-prefix.hyphence"
  printf -- '---\n! md\nX bad\n---\n' >"$f"
  run_hyphence validate "$f"
  assert_failure
  assert_output --partial 'invalid metadata prefix'
}

function validate_rejects_inline_body_with_at { # @test
  # RFC 0001: an `@` blob-reference line in the metadata section is
  # mutually exclusive with an inline body section.
  local f="$BATS_TEST_TMPDIR/inline-and-at.hyphence"
  printf -- '---\n@ blake2b256-abc\n! md\n---\n\nbody\n' >"$f"
  run_hyphence validate "$f"
  assert_failure
  assert_output --partial "blob reference '@' line forbidden"
}

function validate_reads_stdin_with_dash { # @test
  # Reading stdin from a file via `<` is more reliable than a
  # `printf | run ...` pipeline given the run_hyphence wrapper.
  local f="$BATS_TEST_TMPDIR/in.hyphence"
  printf -- '---\n! md\n---\n' >"$f"
  run timeout --preserve-status 2s "$HYPHENCE_BIN" validate - <"$f"
  assert_success
}

function validate_reports_missing_file { # @test
  run_hyphence validate /nonexistent/path/xyz.hyphence
  assert_failure
}

# --- meta ---

function meta_strips_boundaries { # @test
  local f="$BATS_TEST_TMPDIR/m.hyphence"
  printf -- '---\n# desc\n! md\n---\n\nignored\n' >"$f"
  run_hyphence meta "$f"
  assert_success
  assert_output "$(printf '# desc\n! md')"
}

function meta_handles_no_body { # @test
  local f="$BATS_TEST_TMPDIR/m.hyphence"
  printf -- '---\n! md\n---\n' >"$f"
  run_hyphence meta "$f"
  assert_success
  assert_output '! md'
}

# --- body ---

function body_streams_body_bytes { # @test
  local f="$BATS_TEST_TMPDIR/b.hyphence"
  printf -- '---\n! md\n---\n\nhello world\n' >"$f"
  run_hyphence body "$f"
  assert_success
  assert_output 'hello world'
}

function body_empty_when_no_body_section { # @test
  local f="$BATS_TEST_TMPDIR/b.hyphence"
  printf -- '---\n! md\n---\n' >"$f"
  run_hyphence body "$f"
  assert_success
  assert_output ''
}

# --- format ---

function format_canonicalizes { # @test
  local f="$BATS_TEST_TMPDIR/f.hyphence"
  printf -- '---\n! md\n# desc\n---\n\nbody\n' >"$f"
  run_hyphence format "$f"
  assert_success
  assert_output "$(printf -- '---\n# desc\n! md\n---\n\nbody')"
}

function format_is_idempotent { # @test
  local f="$BATS_TEST_TMPDIR/f.hyphence"
  printf -- '---\n! md\n# desc\n- tag\n---\n\nbody\n' >"$f"
  local out1 out2
  out1="$(timeout --preserve-status 2s "$HYPHENCE_BIN" format "$f")"
  out2="$(printf '%s' "$out1" | timeout --preserve-status 2s "$HYPHENCE_BIN" format -)"
  [[ $out1 == "$out2" ]] || fail "format is not idempotent:
first:  $out1
second: $out2"
}

# --- exit-code policy ---

function unknown_subcommand_fails { # @test
  run_hyphence frobnicate
  assert_failure
}
