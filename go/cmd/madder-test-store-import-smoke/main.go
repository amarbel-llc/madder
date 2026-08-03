// Command madder-test-store-import-smoke is the madder#278 regression guard.
//
// It imports ONLY a public store package (pkgs/blob_store_env) — crucially
// NOT markl_registrations — thereby faithfully reproducing an external
// in-process consumer of the madder library. It then asserts that merely
// importing that public package activated madder's markl format/purpose
// registrations: if the age format resolves, the funnel blank-import in
// internal/charlie/blob_store_configs (the #278 fix) is doing its job; if it
// does not, this binary exits nonzero with the #278 error shape.
//
// This is a test fixture, NOT a shipped binary (same not-shipped policy as
// the sibling cmd/madder-test-* servers). Its entire purpose is its import
// set — it must never blank-import markl_registrations, or it would activate
// the registrations itself and stop testing the transitive activation.
package main

import (
	"fmt"
	"os"

	// The ONE activation path under test: importing the PUBLIC store package
	// must transitively activate the markl registrations via the funnel
	// (pkgs/blob_store_env → … → internal/charlie/blob_store_configs, which
	// blank-imports markl_registrations per madder#278). Deliberately blank:
	// this exercises the import side effect, not the package's API.
	_ "code.linenisgreat.com/madder/go/pkgs/blob_store_env"

	"code.linenisgreat.com/piggy/go/pkgs/markl"
)

func main() {
	if _, err := markl.GetFormatOrError(markl.FormatIdAgeX25519Sec); err != nil {
		fmt.Fprintf(
			os.Stderr,
			"madder#278 regression: importing pkgs/blob_store_env did NOT "+
				"activate markl registrations — format %q is unresolved: %v\n",
			markl.FormatIdAgeX25519Sec,
			err,
		)
		os.Exit(1)
	}

	fmt.Println("ok: importing pkgs/blob_store_env transitively activated markl registrations")
}
