package env_local

import (
	"github.com/amarbel-llc/madder/go/internal/delta/env_ui"
	"github.com/amarbel-llc/madder/go/internal/echo/env_dir"
)

type (
	ui  = env_ui.Env
	dir = env_dir.Env
)

type Env interface {
	ui
	dir
}

type env struct {
	ui
	dir
}

func Make(ui ui, dir dir) env {
	return env{
		ui:  ui,
		dir: dir,
	}
}
