package repo_configs

import (
	"github.com/amarbel-llc/madder/go/internal/0/options_print"
	"github.com/amarbel-llc/madder/go/internal/0/options_tools"
	"github.com/amarbel-llc/madder/go/internal/bravo/file_extensions"
	"github.com/amarbel-llc/madder/go/internal/bravo/ids"
)

type Config struct {
	DefaultType    ids.Type
	DefaultTags    ids.TagSet
	FileExtensions file_extensions.Config
	PrintOptions   options_print.Overlay
	ToolOptions    options_tools.Options
}

func MakeConfigFromOverlays(base Config, overlays ...ConfigOverlay) Config {
	return Config{}
}

func (config Config) GetToolOptions() options_tools.Options {
	return config.ToolOptions
}
