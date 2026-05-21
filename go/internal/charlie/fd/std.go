package fd

import (
	"io"
	"os"

	lib_fd "github.com/amarbel-llc/purse-first/libs/dewey/pkgs/fd"
)

type Std = lib_fd.Std

func MakeStd(f *os.File) Std { return lib_fd.MakeStd(f) }

func MakeStdFromWriter(w io.Writer) Std { return lib_fd.MakeStdFromWriter(w) }
