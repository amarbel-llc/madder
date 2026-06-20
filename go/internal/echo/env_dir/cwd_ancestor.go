package env_dir

import (
	"path/filepath"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/xdg"
)

// resolveCwdAncestorOrError walks up depth literal parent directories from
// cwd and returns the resulting ancestor path. It resolves the multi-dot
// cwd id forms for the non-discovery constructor MakeDefaultAndInitialize
// (`.name` -> depth 0, `..name` -> 1, `...name` -> 2; the #145 dot-count
// parsing lives in scoped_id.Id.Set, which stores dots-1 as cwdDepth).
//
// madder#153 (Sasha's ruling, model A + error): the walk is LITERAL —
// every step is one filepath.Dir, with no `.<scope>/` store-existence
// check — and it ERRORS rather than clamps when depth exceeds the
// available ancestors: reaching the filesystem root, or crossing a
// <SCOPE>_CEILING_DIRECTORIES boundary, before depth parents are consumed.
//
// This deliberately diverges from the discovery walk-up
// (directory_layout.FindAllCwdOverridePaths), which is store-aware and
// name-ranked: discovery FINDS existing ancestor stores to operate on,
// whereas this constructor may be INITIALIZING a store that does not
// exist yet, so a store-existence check would be incoherent here. The two
// paths therefore disagree on what `..name` resolves to — resolution is
// literal, discovery is store-aware — by design. depth 0 returns cwd
// unchanged.
func resolveCwdAncestorOrError(
	cwd string,
	depth uint,
	ceilings []string,
) (string, error) {
	dir := cwd

	for consumed := uint(0); consumed < depth; consumed++ {
		parent := filepath.Dir(dir)

		if parent == dir {
			return "", errors.Errorf(
				"cwd dot-depth %d exceeds the available ancestors of %q: "+
					"reached the filesystem root at %q after %d parent(s)",
				depth,
				cwd,
				dir,
				consumed,
			)
		}

		if xdg.IsAboveCeiling(parent, ceilings) {
			return "", errors.Errorf(
				"cwd dot-depth %d exceeds the available ancestors of %q: "+
					"parent %q is above the ceiling after %d parent(s)",
				depth,
				cwd,
				parent,
				consumed,
			)
		}

		dir = parent
	}

	return dir, nil
}
