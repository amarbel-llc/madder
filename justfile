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

# Tag a Go module release. Usage: just tag v0.0.1 "feat: public blob store API"
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

[group("maint")]
gomod2nix:
  cd go && gomod2nix
