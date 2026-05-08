# cutting_garden_plugins

URI-scheme-keyed registry for cutting-garden capture, restore, and
diff backends. Defines the `Plugin`, `CapturePlugin`,
`RestorePlugin`, and `DiffPlugin` interfaces and three independent
package-level registries (one per direction).

Each plugin lives in its own peer-leaf package (e.g.
`cutting_garden_plugin_file`) at hotel or higher and registers
itself in `init()` via `MustRegisterCapture` /
`MustRegisterRestore` / `MustRegisterDiff`. A plugin MAY support
any subset of the three directions; the file plugin happens to
implement all three. The CLI command
(`india/commands_cutting_garden`) blank-imports each plugin to fire
registration at binary startup.

## Layering

Sits at hotel, alongside `blob_transfers`. Imports
`charlie/capture_receipt`, `charlie/capture_sink`, and
`foxtrot/blob_stores`. `india/commands_cutting_garden` imports it;
nothing imports back the other way.

## More information

- `docs/features/0007-cutting-garden-uri-plugins.md` — design
  decisions, scope boundaries, and the cross-scheme constraint
  tracked at #144.
