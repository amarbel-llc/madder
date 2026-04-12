# Plan: Extract Madder from Dodder

## Context

Madder is a content-addressable blob storage CLI + MCP server currently embedded
in dodder (`code.linenisgreat.com/dodder/go`). We're extracting it into its own
repo (`github.com/amarbel-llc/madder`) and switching from dodder's `go/lib/`
general-purpose libraries to purse-first's `dewey` library
(`github.com/amarbel-llc/purse-first/libs/dewey`, on `brave-baobab` branch).

## New Repo Layout

```
madder/
  LICENSE                          # (exists)
  flake.nix                        # gomod2nix build
  flake.lock
  justfile                         # build, test, fmt recipes
  go/
    go.mod                         # module github.com/amarbel-llc/madder
    go.sum
    gomod2nix.toml
    default.nix
    cmd/
      madder/main.go               # entry point
      madder-gen_man/main.go        # generates man1 pages, Source: "Madder"
    internal/
      0/                           # domain_interfaces, options_print, options_tools
      alfa/                        # blob_store_id, repo_id, store_version, string_format_writer
      bravo/                       # descriptions, directory_layout, ids, markl
      charlie/                     # fd, hyphence, repo_config_cli, tap_diagnostics
      delta/                       # blob_store_configs, env_ui
      echo/                        # env_dir
      foxtrot/                     # blob_stores, env_local
      golf/                        # env_repo, man (copied from dodder)
      hotel/                       # command_components_madder, mcp_madder
      india/                       # commands_madder
  docs/
    man.7/                         # madder-relevant man7 sources (blob-store.md, markl-id.md)
```

## Import Rewrite Rules (applied in order)

1. `code.linenisgreat.com/dodder/go/lib/` -> `github.com/amarbel-llc/purse-first/libs/dewey/`
2. `code.linenisgreat.com/dodder/go/internal/golf/command` -> `github.com/amarbel-llc/purse-first/libs/dewey/golf/command`
3. `code.linenisgreat.com/dodder/go/internal/golf/man` -> `github.com/amarbel-llc/madder/go/internal/golf/man`
4. `code.linenisgreat.com/dodder/go/internal/` -> `github.com/amarbel-llc/madder/go/internal/`

go-mcp imports (`github.com/amarbel-llc/purse-first/libs/go-mcp/*`) stay unchanged.

## Steps

### Phase 1: Repo bootstrap

1. **Create `flake.nix`** - inputs: nixpkgs (stable), nixpkgs-master (SHA),
   utils, gomod2nix, purse-first. Build via `buildGoApplication` with
   `subPackages = ["cmd/madder" "cmd/madder-gen_man"]`. Dev shell with go,
   gopls, gofumpt, goimports, gomod2nix, bats.

2. **Create `go/go.mod`** - `module github.com/amarbel-llc/madder`, `go 1.26`.
   Dependencies: dewey (pinned brave-baobab commit), go-mcp, age, zstd, tommy,
   and other transitive deps from dodder's go.mod.

3. **Create `go/default.nix`** - gomod2nix build expression.

4. **Create `justfile`** - `build`, `test`, `fmt`, `tidy` recipes.

### Phase 2: Copy source from dodder

5. **Copy `go/cmd/madder/main.go`** and `go/cmd/madder-gen_man/main.go`.

6. **Copy internal packages** from dodder's `go/internal/` - all packages in
   layers 0 through india that madder depends on (see layout above). These are
   domain-specific blob store packages, not general-purpose lib code.

7. **Do NOT copy `go/lib/`** - these come from dewey now.

### Phase 3: Rewrite imports

8. **Global sed rewrite** across all `.go` files using the 4 rules above (order
   matters).

9. **Run `go build ./...`** - iteratively discover any missing transitive
   internal packages. Copy them from dodder and rewrite their imports.

### Phase 4: Port madder-gen_man and man pages

10. **Copy dodder's `golf/man` package** into madder as `internal/golf/man`.
    It depends on `golf/command` (rewritten to dewey) and `bravo/errors`
    (rewritten to dewey). This is simpler than porting to dewey's
    `App.GenerateManpages()` since the man package is small (4 files) and
    madder's commands use dodder's `Utility` type, not dewey's `App`.

11. **Update `cmd/madder-gen_man/main.go`**: change `Source` from `"Dodder"` to
    `"Madder"`.

12. **Man7 concept pages**: the section 7 pages (blob-store, workspace, hyphence,
    markl-id, etc.) live in dodder's `docs/man.7/` as markdown and are converted
    via pandoc in the nix build. Only `blob-store.md` and `markl-id.md` are
    madder-specific; the rest (workspace, doddish, organize-text, box) are dodder
    concepts. Copy the madder-relevant man7 sources to `docs/man.7/` in this repo.

    **Note**: `.dodder-workspace` is a dodder concept - madder operates on blob
    stores within dodder repos. Do NOT rename workspace references.

### Phase 5: Finalize Go module

13. **`go mod tidy`** to resolve all dependencies.
14. **`gomod2nix`** to generate `gomod2nix.toml`.
15. **`goimports` + `gofumpt`** on all `.go` files.

### Phase 6: Review dodder references in madder code

16. **Do NOT rename `.dodder-workspace`** or other dodder directory/config
    references. Madder is the blob store layer that operates within dodder's
    directory structure. These references are correct.

17. **Review help text only** - ensure madder's own help/description text
    accurately describes itself as a standalone tool, not as part of dodder.

### Phase 7: Build & test

18. **`nix build`** - verify it produces `bin/madder` (gen_man is removed after
    generating pages in postInstall).
19. **`go test ./...`** - run all unit tests.

## Verification

- `nix build` succeeds, produces `bin/madder`, and installs man pages to
  `share/man/man1/` (madder command pages) and `share/man/man7/` (concept pages)
- `go test ./...` passes
- `madder init /tmp/test-repo` creates a blob store
- `echo hello | madder write -repo /tmp/test-repo` -> prints blob ID
- `madder read -repo /tmp/test-repo <id>` -> prints "hello"
- `madder list -repo /tmp/test-repo` -> shows the blob
- `madder fsck -repo /tmp/test-repo` -> reports clean
- `madder mcp` starts and responds to MCP initialize request
- `man madder` shows the man page (once MANPATH propagation is fixed, see eng#25)

## Risks

- **Deep transitive deps**: dodder's internal packages form a deep tree. Phase 3
  step 9 handles this iteratively.
- **dewey on brave-baobab only**: go.mod pins a specific commit. Must update
  when dewey merges to master.
- **Shared code divergence**: domain packages now exist in both repos. Madder
  owns its copies going forward.
- **Man page propagation**: man pages may not be discoverable via `man` until
  eng#25 / dodder#106 are resolved.
