# cutting_garden_plugins

URI-scheme-keyed registry for cutting-garden capture and restore
backends. Defines the `Plugin`, `CapturePlugin`, and `RestorePlugin`
interfaces and the package-level capture/restore registries.

Each plugin lives in its own peer-leaf package (e.g.
`cutting_garden_plugin_file`) at hotel or higher and registers
itself in `init()` via `MustRegisterCapture` / `MustRegisterRestore`.
The CLI command (`india/commands_cutting_garden`) blank-imports each
plugin to fire registration at binary startup.

## Layering

Sits at hotel, alongside `blob_transfers`. Imports
`charlie/capture_receipt`, `charlie/capture_sink`, and
`foxtrot/blob_stores`. `india/commands_cutting_garden` imports it;
nothing imports back the other way.
