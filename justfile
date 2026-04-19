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

# Regenerate pkgs/ facades from internal/ packages via dagnabit.
[group("build")]
generate-facades:
  cd go && dagnabit export

#   _____         _
#  |_   _|__  ___| |_
#    | |/ _ \/ __| __|
#    | |  __/\__ \ |_
#    |_|\___||___/\__|
#

# Run all tests (unit + integration).
[group("test")]
test: test-go test-bats

# Run Go unit tests only.
[group("test")]
test-go *flags:
  cd go && go test -tags test {{flags}} ./...

# Run bats integration tests.
[group("test")]
test-bats: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder just zz-tests_bats/test

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

# Cut a release: assemble a changelog-style message from commits
# since the last go/v* tag, then call `tag` to sign, push, and
# verify. The "go/v" prefix is added for you, so pass the semver
# without it. Usage: just release 0.0.2
#
# Use `just tag <version> <message>` directly if you want to
# control the commit message yourself.
[group("maint")]
release version:
  #!/usr/bin/env bash
  set -euo pipefail
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
  just tag "{{version}}" "$msg"

[group("maint")]
gomod2nix:
  cd go && gomod2nix

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
    -encryption none -lock-internal-files=false

  echo "(tmp workdir: $workdir)"
  echo "(tmp home:    $home)"
