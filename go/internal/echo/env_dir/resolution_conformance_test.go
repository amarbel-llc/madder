//go:build test

package env_dir

// Conformance suite for FDR-0010 (Scoped-ID Resolution),
// docs/features/0010-scoped-id-resolution.md.
//
// These tests are written against the SPEC, not against today's
// behaviour. Some FAIL against the current implementation on purpose:
// each such test documents a Divergence Inventory entry and is marked
// EXPECTED-FAIL via t.Skip with the divergence it pins. Do NOT "fix"
// them by editing the implementation in this pass — they flip to
// passing when the single normative resolver lands (FDR-0010 promotion
// criteria).
//
// The existing env_dir tests pin the CURRENT (divergent) behaviour:
//   - TestResolveCwdAncestorOrError_LiteralWalkUp (literal init walk)
//   - TestMakeDefaultAndInitialize_MultiDotResolvesAncestors
//     (init rooting `..name` at the literal parent)
// The tests below assert the SPEC and will contradict those until the
// resolver is unified; that contradiction is the point of the suite.

import (
	"os"
	"path/filepath"
	"testing"

	"code.linenisgreat.com/madder/go/internal/bravo/directory_layout"
)

// mkStoreTree builds root/mid/leaf and drops a `.madder` marker dir in
// exactly the ancestors named in withMarker. Returns the three paths.
// Non-`default` throughout, per FDR-0010's conformance requirement: the
// existing corpus is almost all "default", the exact blind spot that
// let spec and impl drift.
func mkStoreTree(t *testing.T, withMarker map[string]bool) (root, mid, leaf string) {
	t.Helper()
	root = t.TempDir()
	mid = filepath.Join(root, "mid_dir")
	leaf = filepath.Join(mid, "leaf_dir")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"root": root, "mid": mid, "leaf": leaf} {
		if withMarker[name] {
			if err := os.MkdirAll(filepath.Join(dir, ".madder"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root, mid, leaf
}

// TestConformance_CwdResolvers_AgreeOnContiguousAncestors — PASS anchor.
//
// FDR-0010 "The Normative Resolver": there is ONE resolver, so the init
// walk (resolveCwdAncestorOrError, literal) and the operate walk
// (directory_layout.ResolveNthAncestorMatch, store-aware) MUST return
// the same physical dir for the same id + cwd. When every ancestor
// hosts the store, the literal and store-aware walks coincide — this
// test pins that they already agree there, locating the divergence
// precisely at gapped trees (next test). If this ever fails, the two
// engines have diverged even on the easy case.
func TestConformance_CwdResolvers_AgreeOnContiguousAncestors(t *testing.T) {
	root, mid, leaf := mkStoreTree(t, map[string]bool{"root": true, "mid": true, "leaf": true})
	ceilings := []string{filepath.Dir(root)}
	matchAny := func(string) bool { return true }

	want := []string{leaf, mid, root}
	for depth := uint(0); depth < 3; depth++ {
		lit, litErr := resolveCwdAncestorOrError(leaf, depth, ceilings)
		op, opErr := directory_layout.ResolveNthAncestorMatch(
			leaf, "madder", depth, ceilings, matchAny,
		)
		if litErr != nil || opErr != nil {
			t.Fatalf("depth %d: unexpected error (literal=%v, operate=%v)", depth, litErr, opErr)
		}
		if lit != op {
			t.Errorf("depth %d: resolvers disagree: literal=%q operate=%q", depth, lit, op)
		}
		if op != want[depth] {
			t.Errorf("depth %d: got %q, want %q", depth, op, want[depth])
		}
	}
}

// TestConformance_CwdResolvers_AgreeOnGappedAncestors — EXPECTED-FAIL.
//
// Divergence Inventory #1 (env_dir.MakeDefaultAndInitialize CWD branch,
// resolveCwdAncestorOrError) vs #2 (directory_layout.ResolveNthAncestorMatch).
// When a middle ancestor hosts NO store (a gap), the literal init walk
// counts it (`..name` -> the gap dir) while the store-aware operate walk
// skips it (`..name` -> the next MATCHING ancestor). Same id, same cwd,
// two different physical dirs depending on call site — the core defect
// FDR-0010 removes by adopting the store-aware walk as the one resolver.
//
// Remove this Skip once the unified resolver lands; the body already
// asserts the invariant (both resolvers agree, or both error, at every
// depth).
func TestConformance_CwdResolvers_AgreeOnGappedAncestors(t *testing.T) {
	t.Skip("EXPECTED-FAIL (FDR-0010 Divergence #1/#2): the literal init " +
		"walk (resolveCwdAncestorOrError) counts a store-less gap ancestor; " +
		"the store-aware operate walk (ResolveNthAncestorMatch) skips it, so " +
		"`..name` resolves to two different dirs by call site. Unify onto the " +
		"store-aware walk, then delete this Skip.")

	// mid has NO .madder marker: the gap.
	root, _, leaf := mkStoreTree(t, map[string]bool{"root": true, "mid": false, "leaf": true})
	ceilings := []string{filepath.Dir(root)}
	matchAny := func(string) bool { return true }

	for depth := uint(0); depth < 3; depth++ {
		lit, litErr := resolveCwdAncestorOrError(leaf, depth, ceilings)
		op, opErr := directory_layout.ResolveNthAncestorMatch(
			leaf, "madder", depth, ceilings, matchAny,
		)
		if (litErr == nil) != (opErr == nil) {
			t.Errorf("depth %d: resolvers disagree on error: literal-err=%v operate-err=%v",
				depth, litErr, opErr)
			continue
		}
		if litErr == nil && lit != op {
			t.Errorf("depth %d: resolvers disagree: literal=%q operate=%q", depth, lit, op)
		}
	}
}

// TestConformance_InitResolver_RejectsPositiveDepth — EXPECTED-FAIL.
//
// FDR-0010 "The Init Exception": init MUST create at $PWD and MUST NOT
// derive a creation location from dot-depth; a CWD-scope init with
// depth > 0 (`..name`) MUST be rejected with an actionable error. The
// literal init resolver instead returns the depth-th literal parent (no
// error) — see TestResolveCwdAncestorOrError_LiteralWalkUp and
// TestMakeDefaultAndInitialize_MultiDotResolvesAncestors, which pin that
// current behaviour. This test asserts the SPEC at the closest pure
// proxy: the init walk errors on positive depth. Under the spec the
// literal helper is removed entirely and the rejection lives in
// MakeDefaultAndInitialize's CWD branch; this proxy flips to passing (or
// is replaced by a caller-level assertion) when that lands.
func TestConformance_InitResolver_RejectsPositiveDepth(t *testing.T) {
	t.Skip("EXPECTED-FAIL (FDR-0010 Init Exception / Divergence #1): init " +
		"must reject a positive-depth CWD id (`..name`), not resolve it to a " +
		"literal parent. resolveCwdAncestorOrError returns the parent with no " +
		"error today. Move rejection into the init caller and delete the " +
		"literal helper, then delete this Skip.")

	root, _, leaf := mkStoreTree(t, map[string]bool{"root": true, "mid": true, "leaf": true})
	ceilings := []string{filepath.Dir(root)}

	// depth 1 == `..name`: addressing-only spelling; init must refuse it.
	if _, err := resolveCwdAncestorOrError(leaf, 1, ceilings); err == nil {
		t.Errorf("init resolver accepted a positive-depth (`..name`) id; " +
			"spec requires rejection with an actionable error")
	}
}
