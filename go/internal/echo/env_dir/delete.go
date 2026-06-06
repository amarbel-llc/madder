package env_dir

import (
	"os"
	"path/filepath"

	"github.com/amarbel-llc/purse-first/libs/dewey/pkgs/errors"
)

func (env env) Delete(paths ...string) (err error) {
	for _, path := range paths {
		path = filepath.Clean(path)

		if path == "." {
			err = errors.ErrorWithStackf("invalid delete request: %q", path)
			return err
		}

		if env.IsDryRun() {
			// Route through the per-env sink when one was wired at
			// construction (#232) so library consumers (dodder calls
			// Delete from its checkout/deinit flows) can redirect the
			// notice; nil falls back to the global stderr printer.
			env.getUIErrPrinter().Print("would delete:", path)
			return err
		}

		if err = os.RemoveAll(path); err != nil {
			err = errors.Wrap(err)
			return err
		}
	}

	return err
}
