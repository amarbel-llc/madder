setup() {
  load "$(dirname "$BATS_TEST_FILE")/lib/common.bash"
  export output
}

# bats file_tags=hyphence

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
  local f="$BATS_TEST_TMPDIR/in.hyphence"
  printf -- '---\n! md\n---\n' >"$f"
  run_hyphence validate - <"$f"
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
  local in="$BATS_TEST_TMPDIR/f.hyphence"
  printf -- '---\n! md\n# desc\n- tag\n---\n\nbody\n' >"$in"

  local out1="$BATS_TEST_TMPDIR/out1.hyphence"
  local out2="$BATS_TEST_TMPDIR/out2.hyphence"

  run_hyphence format "$in"
  assert_success
  printf '%s' "$output" >"$out1"

  # Re-run format against out1 via stdin; compare byte-exact.
  run_hyphence format - <"$out1"
  assert_success
  printf '%s' "$output" >"$out2"

  cmp -s "$out1" "$out2" || fail "format is not idempotent (see diff: diff $out1 $out2)"
}

# --- exit-code policy ---

function unknown_subcommand_fails { # @test
  run_hyphence frobnicate
  assert_failure
  assert_output --partial 'unknown command'
}

function validate_rejects_crlf_in_metadata { # @test
  # MetadataValidator rejects \r in metadata lines per RFC 0001
  # (Slice 1 fix in fix(hyphence): reject \r in MetadataBuilder...).
  # Diagnostic surfaces the actual offending line bytes so the user
  # can see the \r — assert on that suffix.
  local f="$BATS_TEST_TMPDIR/crlf.hyphence"
  printf -- '---\r\n! md\r\n---\n' >"$f"
  run_hyphence validate "$f"
  assert_failure
  assert_output --partial 'expected "---" but got "---\r"'
}

function format_anchors_leading_comment { # @test
  # Comments preceding a non-comment metadata line travel with that
  # line through canonicalization.
  local f="$BATS_TEST_TMPDIR/comment.hyphence"
  printf -- '---\n%% about-type\n! md\n---\n' >"$f"
  run_hyphence format "$f"
  assert_success
  assert_output "$(printf -- '---\n%% about-type\n! md\n---')"
}
