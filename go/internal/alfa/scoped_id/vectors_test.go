//go:build test

package scoped_id

// Parser half of the scoped_id grammar-vector conformance suite
// (FDR-0010). Reads testdata/scoped_id_vectors.txt and checks each
// vector's `parser` dimension against the real scoped_id.Id.Set decoder:
// accept (with the expected parsed fields) or reject.
//
// The `grammar` dimension of the SAME corpus is checked separately by
// the langlang grammar-vectors gate (see scoped_id.peg and the
// grammar-vectors nix check). Keeping one corpus with two independently
// pinned outcomes is the hyphence precedent (testdata/rfc_vectors.txt
// feeding both rfc_conformance_test.go and grammar_vectors_test.go).

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"code.linenisgreat.com/madder/go/internal/0/xdg_location_type"
)

type scopedIdVector struct {
	line    int
	input   string
	grammar string // accept | reject (checked by the langlang gate, not here)
	parser  string // accept | reject
	scope   string
	depth   uint
	name    string
	remote  bool
	pin     bool
}

const emptyInputSentinel = "<empty>"

var scopeNameToLocation = map[string]xdg_location_type.Type{
	"user":    xdg_location_type.XDGUser,
	"cwd":     xdg_location_type.Cwd,
	"system":  xdg_location_type.XDGSystem,
	"cache":   xdg_location_type.XDGCache,
	"unknown": xdg_location_type.Unknown,
}

func readScopedIdVectors(t *testing.T) []scopedIdVector {
	t.Helper()

	path := filepath.Join("testdata", "scoped_id_vectors.txt")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() })

	var vectors []scopedIdVector
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		cols := strings.Split(raw, "|")
		for i := range cols {
			cols[i] = strings.TrimSpace(cols[i])
		}
		if len(cols) < 8 {
			t.Fatalf("line %d: expected 8 |-fields, got %d: %q", lineNo, len(cols), raw)
		}

		v := scopedIdVector{
			line:    lineNo,
			input:   cols[0],
			grammar: cols[1],
			parser:  cols[2],
			scope:   cols[3],
			name:    cols[5],
			remote:  cols[6] == "true",
			pin:     cols[7] == "y",
		}
		if v.input == emptyInputSentinel {
			v.input = ""
		}
		if cols[4] != "" {
			d, err := strconv.ParseUint(cols[4], 10, 64)
			if err != nil {
				t.Fatalf("line %d: bad depth %q: %v", lineNo, cols[4], err)
			}
			v.depth = uint(d)
		}
		vectors = append(vectors, v)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if len(vectors) == 0 {
		t.Fatalf("no vectors read from %s", path)
	}
	return vectors
}

// TestScopedIdVectors_Parser checks the `parser` dimension of every
// corpus vector against scoped_id.Id.Set.
func TestScopedIdVectors_Parser(t *testing.T) {
	for _, v := range readScopedIdVectors(t) {
		v := v
		t.Run(strconv.Itoa(v.line)+"/"+v.input, func(t *testing.T) {
			var id Id
			err := id.Set(v.input)

			switch v.parser {
			case "reject":
				if err == nil {
					t.Fatalf("input %q: expected parser REJECT, but Set succeeded (%+v)", v.input, id)
				}

			case "accept":
				if err != nil {
					t.Fatalf("input %q: expected parser ACCEPT, but Set failed: %v", v.input, err)
				}
				wantLoc, ok := scopeNameToLocation[v.scope]
				if !ok {
					t.Fatalf("input %q: unknown scope name %q in corpus", v.input, v.scope)
				}
				if id.GetLocationType() != wantLoc {
					t.Errorf("input %q: scope = %v, want %v (%s)", v.input, id.GetLocationType(), wantLoc, v.scope)
				}
				if id.GetCwdDepth() != v.depth {
					t.Errorf("input %q: cwdDepth = %d, want %d", v.input, id.GetCwdDepth(), v.depth)
				}
				if id.GetName() != v.name {
					t.Errorf("input %q: name = %q, want %q", v.input, id.GetName(), v.name)
				}
				if id.IsRemoteFirst() != v.remote {
					t.Errorf("input %q: remoteFirst = %v, want %v", v.input, id.IsRemoteFirst(), v.remote)
				}
				if id.HasDigest() != v.pin {
					t.Errorf("input %q: HasDigest = %v, want %v", v.input, id.HasDigest(), v.pin)
				}

			default:
				t.Fatalf("input %q: unrecognized parser outcome %q in corpus", v.input, v.parser)
			}
		})
	}
}
