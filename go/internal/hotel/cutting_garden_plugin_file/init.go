package cutting_garden_plugin_file

import (
	"github.com/amarbel-llc/madder/go/internal/hotel/cutting_garden_plugins"
)

func init() {
	p := Plugin{}
	cutting_garden_plugins.MustRegisterCapture(p)
	cutting_garden_plugins.MustRegisterRestore(p)
}
