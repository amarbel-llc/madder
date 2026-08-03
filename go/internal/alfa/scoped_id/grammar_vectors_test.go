//go:build test

package scoped_id

// Grammar half of the scoped_id grammar-vector conformance suite
// (FDR-0010): runs each corpus vector's `grammar` dimension through
// langlang against scoped_id.peg, the structural counterpart to
// vectors_test.go's parser (Id.Set) half. Mirrors hyphence's
// grammar_vectors_test.go (hyphence#9).
//
// Skips (does not fail) when langlang or the staged grammar aren't
// available, so a plain `go test -tags test ./...` outside the nix-wired
// gate still passes. The enforced check is `nix build .#grammar-vectors`
// / `just test-grammar-vectors`, which builds the langlang binary from
// the flake-input-go_mod-bridged input and sets LANGLANG_BIN +
// SCOPED_ID_GRAMMAR_PEG (the latter pointing at scoped_id.peg staged
// beside piggy's marklid.peg, since the peg `@import`s it and langlang
// resolves `@import` relative to the importing file).

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// scopedIdStartRule is scoped_id.peg's first (start) rule. langlang's
// -input mode always parses against the grammar's start rule; a
// successful parse's stdout is a tree dump rooted at that name, and a
// failed parse instead prints a `path:line:col: message` line (langlang
// exits 0 either way, so the output — not the exit code — is the signal).
const scopedIdStartRule = "ScopedId"

var (
	scopedIdLanglangFailurePattern = regexp.MustCompile(`^\S+:\d+:\d+: `)
	// langlang's tree dump is ANSI-colored; strip SGR escapes before the
	// literal-prefix check or the escape sequence is seen first.
	scopedIdANSISGRPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")
)

func resolveScopedIdLanglangBin() (string, bool) {
	if bin := os.Getenv("LANGLANG_BIN"); bin != "" {
		return bin, true
	}
	if p, err := exec.LookPath("langlang"); err == nil {
		return p, true
	}
	return "", false
}

// resolveScopedIdGrammarPeg returns the STAGED scoped_id.peg (co-located
// with piggy's marklid.peg, which it `@import`s). There is no in-tree
// fallback: the bare scoped_id.peg's relative `@import` cannot resolve
// without marklid.peg staged beside it, so the check is meaningful only
// under the nix gate that provides SCOPED_ID_GRAMMAR_PEG.
func resolveScopedIdGrammarPeg() (string, bool) {
	if p := os.Getenv("SCOPED_ID_GRAMMAR_PEG"); p != "" {
		return p, true
	}
	return "", false
}

// TestScopedIdGrammarVectors checks the `grammar` dimension of every
// corpus vector against scoped_id.peg via langlang -input.
func TestScopedIdGrammarVectors(t *testing.T) {
	langlangBin, ok := resolveScopedIdLanglangBin()
	if !ok {
		t.Skip("langlang not available (set LANGLANG_BIN); enforced gate is `nix build .#grammar-vectors`")
	}
	grammarPeg, ok := resolveScopedIdGrammarPeg()
	if !ok {
		t.Skip("staged scoped_id.peg not available (set SCOPED_ID_GRAMMAR_PEG); enforced gate is `nix build .#grammar-vectors`")
	}

	for _, v := range readScopedIdVectors(t) {
		v := v
		t.Run(strconv.Itoa(v.line)+"/"+v.input, func(t *testing.T) {
			parsed, detail := scopedIdParsesUnderGrammar(t, langlangBin, grammarPeg, v.input)
			switch v.grammar {
			case "accept":
				if !parsed {
					t.Errorf("input %q: expected grammar ACCEPT under scoped_id.peg, but it did not parse:\n%s", v.input, detail)
				}
			case "reject":
				if parsed {
					t.Errorf("input %q: expected grammar REJECT under scoped_id.peg, but it parsed", v.input)
				}
			default:
				t.Fatalf("input %q: unrecognized grammar outcome %q in corpus", v.input, v.grammar)
			}
		})
	}
}

// scopedIdParsesUnderGrammar runs one scoped-id string through langlang
// against scoped_id.peg and reports whether it parsed cleanly against the
// ScopedId start rule (which anchors EOF, so a partial match leaving
// trailing input fails). stdout/stderr are kept separate: the tree-dump
// prefix check keys off stdout, and interleaved stderr could break it.
func scopedIdParsesUnderGrammar(t *testing.T, langlangBin, grammarPeg, content string) (bool, string) {
	t.Helper()

	tmp, err := os.CreateTemp(t.TempDir(), "scoped-id-grammar-vector-*")
	if err != nil {
		t.Fatalf("create temp input: %v", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatalf("write temp input: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp input: %v", err)
	}

	cmd := exec.Command(
		langlangBin,
		"-grammar", grammarPeg,
		"-input", tmp.Name(),
		"-disable-builtins",
		"-disable-spaces",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmdErr := cmd.Run()
	trimmed := strings.TrimSpace(scopedIdANSISGRPattern.ReplaceAllString(stdout.String(), ""))
	detail := "stdout: " + stdout.String() + "\nstderr: " + stderr.String()

	if cmdErr != nil ||
		trimmed == "" ||
		scopedIdLanglangFailurePattern.MatchString(trimmed) ||
		!strings.HasPrefix(trimmed, scopedIdStartRule) {
		return false, detail
	}

	return true, detail
}
