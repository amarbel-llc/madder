package env_dir

import (
	"fmt"

	"code.linenisgreat.com/purse-first/libs/dewey/pkgs/errors"
)

type (
	pkgErrDisamb struct{}
	pkgError     = errors.Typed[pkgErrDisamb]
)

func MakeErrTempAlreadyExists(
	path string,
) (err ErrTempAlreadyExists) {
	err = ErrTempAlreadyExists{Path: path}
	return err
}

var _ errors.Helpful = ErrTempAlreadyExists{}

type ErrTempAlreadyExists struct {
	Path string
}

func (err ErrTempAlreadyExists) Error() string {
	return fmt.Sprintf("Local temporary directory already exists: %q", err.Path)
}

func (err ErrTempAlreadyExists) GetErrorCause() []string {
	return []string{
		"Another dodder previous process with the same PID likely terminated unexpectedly",
	}
}

func (err ErrTempAlreadyExists) GetErrorRecovery() []string {
	return []string{
		"Check if there are any relevant files in the directory, or possible delete it",
	}
}

func (err ErrTempAlreadyExists) Is(target error) bool {
	_, ok := target.(ErrTempAlreadyExists)
	return ok
}

func (err ErrTempAlreadyExists) GetErrorType() pkgErrDisamb {
	return pkgErrDisamb{}
}
