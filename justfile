set dotenv-load

default: build test

#   ____        _ _     _
#  | __ ) _   _(_) | __| |
#  |  _ \| | | | | |/ _` |
#  | |_) | |_| | | | (_| |
#  |____/ \__,_|_|_|\__,_|
#

[group("build")]
build:
  nix build --show-trace

[group("build")]
build-go:
  cd go && go build ./...

# Build race-instrumented CLI binaries under .tmp/race/, for use by
# test-bats-race. Kept out of the default build because the race
# instrumentation is slow and the binaries are unsuitable for release.
[group("build")]
build-go-race:
  mkdir -p {{justfile_directory()}}/.tmp/race
  cd go && go build -race -o {{justfile_directory()}}/.tmp/race/ ./cmd/madder ./cmd/madder-cache

# Build coverage-instrumented CLI binaries under .tmp/cover-bin/, for
# use by test-bats-cover. The -cover flag wires the binaries to write
# per-process coverage to $GOCOVERDIR at runtime.
[group("build")]
build-go-cover:
  mkdir -p {{justfile_directory()}}/.tmp/cover-bin
  cd go && go build -cover -covermode=atomic -o {{justfile_directory()}}/.tmp/cover-bin/ ./cmd/madder ./cmd/madder-cache

# Regenerate pkgs/ facades from internal/ packages via dagnabit.
[group("build")]
generate-facades:
  cd go && dagnabit export

# Regenerate tommy codegen output for the blob_store_configs package.
# Deletes all *_tommy.go files first so that tommy's analyze step sees
# only the hand-written structs as source of truth — decoupling
# regeneration from whatever stale state the previous generated files
# happened to be in.
[group("build")]
generate-tommy:
  find {{justfile_directory()}}/go/internal/charlie/blob_store_configs \
    -maxdepth 1 -type f -name '*_tommy.go' -delete
  cd go && go generate ./internal/charlie/blob_store_configs/...
  goimports -w {{justfile_directory()}}/go/internal/charlie/blob_store_configs/*_tommy.go

#   _____         _
#  |_   _|__  ___| |_
#    | |/ _ \/ __| __|
#    | |  __/\__ \ |_
#    |_|\___||___/\__|
#

# Run all tests (unit + integration) with the race detector enabled.
# Concurrent-write paths (pack_parallel, blob_mover link publish) are
# load-bearing, so default verification runs them under -race.
[group("test")]
test: test-go-race test-bats test-bats-net-cap

# Run Go unit tests only.
[group("test")]
test-go *flags:
  cd go && go test -tags test {{flags}} ./...

# Run `go vet` across the module with the test build tag, which gates
# several internal test-only symbols. Without -tags test, vet reports
# false positives on test-tagged source files.
[group("test")]
vet-go *flags:
  cd go && go vet -tags test {{flags}} ./...

# Build, vet, and test a single internal subpackage tree — the standard
# verification triple, but scoped to ./internal/<subpath>/... so we don't
# wait for the whole module when iterating on one package.
# Usage: just verify-internal-pkg futility
[group("test")]
verify-internal-pkg subpath:
  cd go && go build ./internal/{{subpath}}/...
  cd go && go vet -tags test ./internal/{{subpath}}/...
  cd go && go test -tags test ./internal/{{subpath}}/...

# Run Go unit tests under the race detector. Invoked by the default
# `test` target; kept as a standalone recipe for flag-passing use cases.
[group("test")]
test-go-race *flags:
  cd go && go test -tags test -race {{flags}} ./...

# Run Go unit tests with coverage collection. Writes the profile to
# .tmp/go-cover.out and prints the total coverage. View the full HTML
# report with `cd go && go tool cover -html=../.tmp/go-cover.out`.
[group("test")]
test-go-cover *flags:
  #!/usr/bin/env bash
  set -euo pipefail
  out="{{justfile_directory()}}/.tmp/go-cover.out"
  mkdir -p "$(dirname "$out")"
  cd go
  go test -tags test -coverprofile="$out" -covermode=atomic {{flags}} ./...
  echo "==> Coverage written to $out"
  go tool cover -func="$out" | tail -n 1

# Run bats integration tests. Excludes net_cap-tagged tests (loopback-
# binding scenarios) — those run under `test-bats-net-cap`.
[group("test")]
test-bats: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder just zz-tests_bats/test

# Run net_cap-tagged bats tests under sandcastle's --allow-local-binding.
# Currently covers the SFTP harness; future loopback-binding harnesses
# (HTTP, CalDAV, etc.) get the same treatment.
[group("test")]
test-bats-net-cap: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder just zz-tests_bats/test-net-cap

# Run bats integration tests against race-instrumented binaries built
# by build-go-race. Catches data races that the unit-test -race pass
# won't, since several code paths only execute in the real CLI.
[group("test")]
test-bats-race: build-go-race
  MADDER_BIN={{justfile_directory()}}/.tmp/race/madder just zz-tests_bats/test

# Run bats integration tests against coverage-instrumented binaries.
# Collects GOCOVERDIR fragments into a per-run temp dir and converts
# them to a Go coverage profile at .tmp/cover-data/bats-coverage.out.
#
# GOCOVERDIR is placed under /tmp explicitly (not TMPDIR) because the
# bats sandcastle harness restricts the binary's write roots to /tmp
# and the test's own BATS_TEST_TMPDIR. A GOCOVERDIR inside the repo's
# .tmp/ (which is where $TMPDIR often points in nix-shell) trips
# "read-only file system" errors from the cover runtime.
[group("test")]
test-bats-cover: build-go-cover
  #!/usr/bin/env bash
  set -euo pipefail
  cover_data="$(mktemp -d /tmp/madder-cover-XXXXXX)"
  trap 'rm -rf "$cover_data"' EXIT

  GOCOVERDIR="$cover_data" \
    MADDER_BIN="{{justfile_directory()}}/.tmp/cover-bin/madder" \
    just zz-tests_bats/test

  out_dir="{{justfile_directory()}}/.tmp/cover-data"
  mkdir -p "$out_dir"
  (cd go && go tool covdata textfmt -i="$cover_data" -o="$out_dir/bats-coverage.out")
  echo "==> Coverage written to $out_dir/bats-coverage.out"
  (cd go && go tool cover -func="$out_dir/bats-coverage.out" | tail -n 1)

# Run specific bats test files.
[group("test")]
test-bats-targets *targets: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder just zz-tests_bats/test-targets {{targets}}

# Run bats tests filtered by tag.
[group("test")]
test-bats-tags *tags: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder just zz-tests_bats/test-tags {{tags}}

#   _____                          _
#  |  ___|__  _ __ _ __ ___   __ _| |_
#  | |_ / _ \| '__| '_ ` _ \ / _` | __|
#  |  _| (_) | |  | | | | | | (_| | |_
#  |_|  \___/|_|  |_| |_| |_|\__,_|\__|
#

[group("fmt")]
fmt:
  cd go && goimports -w .
  cd go && gofumpt -w .

#   __  __       _       _
#  |  \/  | __ _(_)_ __ | |_
#  | |\/| |/ _` | | '_ \| __|
#  | |  | | (_| | | | | | |_
#  |_|  |_|\__,_|_|_| |_|\__|
#

[group("maint")]
tidy:
  cd go && go mod tidy

# Update dewey to a version (e.g. just update-dewey v0.0.3).
[group("maint")]
update-dewey version:
  cd go && go get github.com/amarbel-llc/purse-first/libs/dewey@{{version}} && go mod tidy
  just gomod2nix

# Tag a Go module release. The "go/v" prefix is added for you, so pass
# the semver without it. Usage: just tag 0.0.1 "feat: public blob store API"
[group("maint")]
tag version message:
  #!/usr/bin/env bash
  set -euo pipefail
  tag="go/v{{version}}"
  prev=$(git tag --sort=-v:refname -l "go/v*" | head -1)
  if [[ -n "$prev" ]]; then
    gum log --level info "Previous: $prev"
    git log --oneline "$prev"..HEAD -- go/
  fi
  git tag -s -m "{{message}}" "$tag"
  gum log --level info "Created tag: $tag"
  git push origin "$tag"
  gum log --level info "Pushed $tag"
  git tag -v "$tag"

# Sed-rewrite madderVersion in flake.nix to the given semver. The
# version string is burnt into the binary at build time via -ldflags
# (see go/internal/0/buildinfo), so flake.nix is the single source of
# truth. No-op if already at the target version. Usage: just
# bump-version 0.0.2
[group("maint")]
bump-version new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  current=$(grep 'madderVersion = ' flake.nix | sed 's/.*"\(.*\)".*/\1/')
  if [[ "$current" == "{{new_version}}" ]]; then
    gum log --level info "already at {{new_version}}"
    exit 0
  fi
  sed -i.bak 's/madderVersion = "'"$current"'"/madderVersion = "{{new_version}}"/' flake.nix && rm flake.nix.bak
  gum log --level info "bumped madderVersion: $current → {{new_version}}"

# Cut a release: must be run on master. Bumps madderVersion in
# flake.nix, commits the bump with a changelog-style message built
# from commits since the last go/v* tag, pushes master, then signs
# and pushes the go/v{{version}} tag. The "go/v" prefix is added for
# you, so pass the semver without it. Usage: just release 0.0.2
#
# Use `just tag <version> <message>` directly if you want to
# control the commit message yourself without bumping.
[group("maint")]
release version:
  #!/usr/bin/env bash
  set -euo pipefail
  current_branch=$(git rev-parse --abbrev-ref HEAD)
  if [[ "$current_branch" != "master" ]]; then
    gum log --level error "just release must be run on master (currently on $current_branch)"
    exit 1
  fi
  prev=$(git tag --sort=-v:refname -l "go/v*" | head -1)
  header="release v{{version}}"
  if [[ -n "$prev" ]]; then
    summary=$(git log --format='- %s' "$prev"..HEAD -- go/)
    if [[ -n "$summary" ]]; then
      msg="$header"$'\n\n'"$summary"
    else
      msg="$header"
    fi
  else
    msg="$header"
  fi
  just bump-version "{{version}}"
  if ! git diff --quiet flake.nix; then
    git add flake.nix
    git commit -m "chore: release go/v{{version}}"
    git push origin master
    gum log --level info "pushed flake.nix bump to master"
  fi
  just tag "{{version}}" "$msg"

[group("maint")]
gomod2nix:
  cd go && gomod2nix

# Print the version subcommand output from the nix-built binaries.
# Used to verify -ldflags injection (see go/internal/0/buildinfo).
[group("debug")]
debug-version:
  #!/usr/bin/env bash
  set -euo pipefail
  just build >/dev/null
  echo "madder:       $({{justfile_directory()}}/result/bin/madder version)"
  echo "madder-cache: $({{justfile_directory()}}/result/bin/madder-cache version)"

# Copy a subpackage from the cached dewey module into go/internal/<dest>/
# so we can iterate on it in-tree without a purse-first release cycle.
# Resolves the pinned dewey version from go.mod, locates it in GOMODCACHE,
# copies recursively, and chmods writable. Destination must not exist.
# Usage: just incubate-dewey-pkg golf/command futility
[group("debug")]
incubate-dewey-pkg subpath dest:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  mod_cache=$(cd go && go env GOMODCACHE)
  ver=$(cd go && go list -m -f '{{{{.Version}}}}' github.com/amarbel-llc/purse-first/libs/dewey)
  src="$mod_cache/github.com/amarbel-llc/purse-first/libs/dewey@${ver}/{{subpath}}"
  dst="go/internal/{{dest}}"
  if [ ! -d "$src" ]; then
    echo "source not found: $src" >&2
    exit 1
  fi
  if [ -e "$dst" ]; then
    echo "destination already exists: $dst (remove or choose another name)" >&2
    exit 1
  fi
  cp -r "$src" "$dst"
  chmod -R u+w "$dst"
  echo "copied $src -> $dst"

# Rewrite an import path and its unqualified package identifier across
# every .go file in the module. Skips files whose path matches any of the
# optional `skip` arguments (find's -path form, trailing /* for subtrees).
# Use to migrate consumers after moving a package (e.g. incubation ->
# upstream swap, or this repo's golf/command -> futility cutover).
# Usage:
#   just rename-go-import \
#     github.com/amarbel-llc/madder/go/internal/golf/command \
#     github.com/amarbel-llc/madder/go/internal/futility \
#     command futility \
#     'go/internal/golf/command/*' 'go/internal/futility/*'
[group("debug")]
rename-go-import old_path new_path old_ident new_ident *skips:
  #!/usr/bin/env bash
  set -euo pipefail
  set -f  # disable globbing so skip patterns with `*` aren't expanded against CWD
  cd {{justfile_directory()}}
  args=(go -name '*.go' -type f)
  for skip in {{skips}}; do
    args+=(! -path "$skip")
  done
  set +f
  find "${args[@]}" -print0 | xargs -0 sed -i \
    -e "s|{{old_path}}\"|{{new_path}}\"|g" \
    -e "s|{{old_path}}/|{{new_path}}/|g" \
    -e "s/\\b{{old_ident}}\\.\\([A-Z]\\)/{{new_ident}}.\\1/g"
  echo "rewrote import {{old_path}} -> {{new_path}} and {{old_ident}}.* -> {{new_ident}}.*"

# Rewrite `package <old>` → `package <new>` in every .go file under a
# directory tree. Also rewrites `package <old>_test` for external test
# files. Does not touch import statements — rename consumers separately.
# Usage: just rename-go-package go/internal/futility command futility
[group("debug")]
rename-go-package dir old new:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  if [ ! -d "{{dir}}" ]; then
    echo "not a directory: {{dir}}" >&2
    exit 1
  fi
  find "{{dir}}" -name '*.go' -type f -print0 | xargs -0 sed -i \
    -e 's/^package {{old}}$/package {{new}}/' \
    -e 's/^package {{old}}_test$/package {{new}}_test/'
  echo "renamed package {{old}} -> {{new}} in {{dir}}"

# Regenerate man pages into a tmp dir and print one by name. Usage: just debug-gen_man madder.1
[group("debug")]
debug-gen_man page="madder.1":
  #!/usr/bin/env bash
  set -euo pipefail
  out=$(mktemp -d)
  cd go && go run ./cmd/madder-gen_man "$out"
  cat "$out/share/man/man1/{{page}}"

# Repro for #21: try `madder init` with and without the flags bats uses.
# Runs in an isolated tmp HOME/workdir under a ceiling that prevents madder
# from walking into any real config. All variants keep MADDER_CEILING_DIRECTORIES
# pinned at the tmp workdir — do not remove it, see note below. Usage:
# just debug-init-repro [storeid]
#
# Safety: without a ceiling, madder walks CWD upward looking for
# .madder-workspace/.dodder-workspace. Because this recipe's tmp workdir
# lives under the repo's .tmp/, an uncapped walk would reach the repo root
# and potentially the host's $HOME. Every variant sets the ceiling.
[group("debug")]
debug-init-repro storeid="default":
  #!/usr/bin/env bash
  set -u
  root={{justfile_directory()}}
  run_case() {
    local label="$1"; shift
    local storeid="$1"; shift
    echo "=== $label ==="
    echo "  env:"
    echo "    HOME=${HOME:-<unset>}"
    echo "    XDG_CONFIG_HOME=${XDG_CONFIG_HOME:-<unset>}"
    echo "    XDG_DATA_HOME=${XDG_DATA_HOME:-<unset>}"
    echo "    MADDER_CEILING_DIRECTORIES=${MADDER_CEILING_DIRECTORIES:-<unset>}"
    echo "    CWD=$(pwd)"
    set +e
    (cd "$root/go" && go run ./cmd/madder init "$@" "$storeid")
    local rc=$?
    set -e
    echo "  exit=$rc"
    echo
  }

  home=$(mktemp -d)
  workdir=$(mktemp -d)
  cd "$workdir"

  # Scrub any inherited XDG_* so variants that claim "XDG_* unset" really
  # start clean. Set HOME to tmp everywhere so defaulted XDG paths fall
  # under tmp (not the host user's ~).
  unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME XDG_STATE_HOME
  export HOME="$home"
  export MADDER_CEILING_DIRECTORIES="$workdir"

  # Variant A: all XDG_* explicitly set to subpaths of tmp home.
  XDG_CONFIG_HOME="$home/.config" XDG_DATA_HOME="$home/.local/share" \
    XDG_CACHE_HOME="$home/.cache" XDG_STATE_HOME="$home/.local/state" \
    run_case "A: all XDG set + ceiling, bare init" "{{storeid}}A"

  # Variant B: XDG_* unset (Go will default to $HOME subpaths).
  run_case "B: XDG_* unset + ceiling, bare init" "{{storeid}}B"

  # Variant C: XDG_* unset + the bats workaround flags.
  run_case "C: XDG_* unset + ceiling, workaround flags" "{{storeid}}C" \
    -encryption none

  echo "(tmp workdir: $workdir)"
  echo "(tmp home:    $home)"
