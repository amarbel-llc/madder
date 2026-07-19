package env_local

//go:generate dagnabit export

import (
	"code.linenisgreat.com/madder/go/internal/delta/env_ui"
	"code.linenisgreat.com/madder/go/internal/echo/env_dir"
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
