set dotenv-load

default: lint build test

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
# dagnabit now emits conformist-compatible output directly — it folds
# consecutive same-kind decls into grouped blocks and runs the project
# formatter on its output (amarbel-llc/purse-first#108, since closed), so
# a fresh export is already lint-fmt-clean. No post-export `nix fmt` is
# needed: `dagnabit export` alone leaves pkgs/ byte-identical to a
# committed, formatted tree (this is also what `lint-facades` relies on).
[group("build")]
generate-facades:
  cd go && dagnabit export

# Regenerate facades and show what dewey imports landed in the three
# facades that import dewey directly (domain_interfaces, hyphence,
# markl). Used to debug dagnabit import resolution (e.g. internal/ vs
# pkgs/ path choice). Depends on generate-facades so regeneration is
# expressed once, not duplicated inline.
[group("debug")]
debug-check-facade-imports: generate-facades
  grep purse-first go/pkgs/domain_interfaces/main.go || true
  grep purse-first go/pkgs/hyphence/main.go || true
  grep purse-first go/pkgs/markl/main.go || true

# Regenerate tommy codegen output for the blob_store_configs package.
# Deletes all *_tommy.go files first so that tommy's analyze step sees
# only the hand-written structs as source of truth — decoupling
# regeneration from whatever stale state the previous generated files
# happened to be in.
#
# Chains `conformist` last (like generate-facades): tommy emits no blank
# lines between top-level functions, but gofumpt restores them, so the
# raw `goimports -w` output is not conformist-clean. Running the formatter
# here keeps generate-tommy a one-step process and prevents merge-hook
# (lint-fmt) surprises.
#
# Uses the `tommy` CLI from the devshell (the tommy flake input). The CLI
# can't be `go build`'d from madder's own module: tommy's codegen packages
# pull deps (e.g. dave/jennifer) that madder doesn't import, so `go mod
# tidy` prunes them from go.sum and an in-module build fails. Keep the
# devshell tommy in sync with go.mod's tommy via `nix flake update tommy`.
[group("build")]
generate-tommy:
  find {{justfile_directory()}}/go/internal/charlie/blob_store_configs \
    -maxdepth 1 -type f -name '*_tommy.go' -delete
  cd go && go generate ./internal/charlie/blob_store_configs/...
  goimports -w {{justfile_directory()}}/go/internal/charlie/blob_store_configs/*_tommy.go
  nix develop {{justfile_directory()}} --command conformist

#    ____ _
#   / ___| | ___  __ _ _ __
#  | |   | |/ _ \/ _` | '_ \
#  | |___| |  __/ (_| | | | |
#   \____|_|\___|\__,_|_| |_|
#

# Wipe Go's build cache. Useful when bisecting a stale-build mystery
# or recovering from a corrupted cache entry.
[group("maintenance")]
clean-go-cache:
  cd go && go clean -cache

# Wipe Go's module cache (~/go/pkg/mod). Forces re-download of every
# module on the next build. Heavier than clean-go-cache.
[group("maintenance")]
clean-go-modcache:
  cd go && go clean -modcache

# Both Go cleans together.
[group("maintenance")]
clean-go: clean-go-cache clean-go-modcache

# Remove the nix-build symlink. Forces the next `nix build` to
# refresh the symlink even if its store path is reachable from cache.
[group("maintenance")]
clean-nix-result:
  rm -f {{justfile_directory()}}/result

# Full sweep: Go caches + nix-build symlink. Recovers from most
# stale-build states without touching git-tracked files or the nix
# store (use `nix store gc` for the latter). Does NOT wipe .tmp/
# — long-running clients (Clown, devshell tools) hold open files
# there and rely on $TMPDIR pointing at it.
[group("maintenance")]
clean: clean-go clean-nix-result

#   _____         _
#  |_   _|__  ___| |_
#    | |/ _ \/ __| __|
#    | |  __/\__ \ |_
#    |_|\___||___/\__|
#

# Run all tests (unit + integration) with the race detector enabled.
# Concurrent-write paths (pack_parallel, blob_mover link publish) are
# load-bearing, so default verification runs them under -race.
# vet-go-analyzers runs first because it's cheap and catches issues
# the test suites would not (deferred error drops, pool leaks,
# discarded iter.Seq2 errors).
[group("post-build")]
test: vet-go-analyzers test-go-race test-bats test-bats-net-cap

# Run Go unit tests only.
[group("post-build")]
test-go *flags:
  cd go && go test -tags test {{flags}} ./...

# Run Go benchmarks. Usage: just bench-go ./internal/foxtrot/blob_stores
# Defaults: -benchtime=1x for a fast smoke run; pass `-benchtime=3s` etc.
# in flags for real timing. -run=^$ suppresses test functions.
[group("post-build")]
bench-go pkg="./..." *flags="-benchtime=1x":
  cd go && go test -tags test -run=^$ -bench=. {{flags}} {{pkg}}

# Run `go vet` across the module with the test build tag, which gates
# several internal test-only symbols. Without -tags test, vet reports
# false positives on test-tagged source files.
[group("post-build")]
vet-go *flags:
  cd go && go vet -tags test {{flags}} ./...

# Build a dewey go/analysis analyzer (defererr, repool, or seqerror)
# from the module cache into .tmp/analyzers/, then run it via
# `go vet -vettool`. Strict: any analyzer finding fails the recipe.
# The analyzer cmds are pinned via go.mod `tool` directives so
# `go mod tidy` does not drop their transitive deps.
[group("post-build")]
vet-go-analyzer name:
  #!/usr/bin/env bash
  set -euo pipefail
  bin="{{justfile_directory()}}/.tmp/analyzers/{{name}}"
  mkdir -p "$(dirname "$bin")"
  cd go
  go build -o "$bin" github.com/amarbel-llc/purse-first/libs/dewey/cmd/{{name}}
  go vet -tags test -vettool="$bin" ./...

# Run every dewey analyzer in sequence. Wired into the top-level
# `test` aggregate; runs first so analyzer findings break the loop
# before the slower bats lanes start.
[group("post-build")]
vet-go-analyzers: (vet-go-analyzer "seqerror") (vet-go-analyzer "repool") (vet-go-analyzer "defererr")

# Build, vet, and test a single internal subpackage tree — the standard
# verification triple, but scoped to ./internal/<subpath>/... so we don't
# wait for the whole module when iterating on one package.
# Usage: just verify-internal-pkg futility
[group("post-build")]
verify-internal-pkg subpath:
  cd go && go build ./internal/{{subpath}}/...
  cd go && go vet -tags test ./internal/{{subpath}}/...
  cd go && go test -tags test ./internal/{{subpath}}/...

# Run Go unit tests under the race detector. Invoked by the default
# `test` target; kept as a standalone recipe for flag-passing use cases.
[group("post-build")]
test-go-race *flags:
  cd go && go test -tags test -race {{flags}} ./...

# Run Go unit tests with coverage collection. Writes covdata fragments
# to .tmp/cover-data/unit/ (mergeable with the bats lane via cover-all)
# and a textfmt profile to .tmp/go-cover.out (the legacy interface).
# View the full HTML report with
# `cd go && go tool cover -html=../.tmp/go-cover.out`.
[group("post-build")]
test-go-cover *flags:
  #!/usr/bin/env bash
  set -euo pipefail
  unit_dir="{{justfile_directory()}}/.tmp/cover-data/unit"
  out="{{justfile_directory()}}/.tmp/go-cover.out"
  rm -rf "$unit_dir"
  mkdir -p "$unit_dir" "$(dirname "$out")"
  cd go
  go test -tags test -cover -covermode=atomic {{flags}} ./... \
    -args -test.gocoverdir="$unit_dir" >/dev/null
  go tool covdata textfmt -i="$unit_dir" -o="$out"
  echo "==> Coverage written to $out (fragments at $unit_dir)"
  go tool cover -func="$out" | tail -n 1

# Run bats integration tests. Excludes net_cap-tagged tests (loopback-
# binding scenarios) — those run under `test-bats-net-cap`.
[group("post-build")]
test-bats: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder \
    HYPHENCE_BIN={{justfile_directory()}}/result/bin/hyphence \
    just zz-tests_bats/test

# Run net_cap-tagged bats tests (SFTP + WebDAV harnesses, future
# loopback-binding harnesses) via the nix-sandbox lane. The lane bundles
# MADDER_TEST_SFTP_SERVER / MADDER_TEST_CRAFT_LEGACY_BLOB /
# MADDER_TEST_WEBDAV_SERVER into its binaries map (see
# `netCapExtraBinaries` in go/default.nix), so the lane is
# self-sufficient without a devshell-side fixture-binary spawn.
#
# The nix sandbox provides a fresh network namespace with loopback up,
# which is everything the SFTP/WebDAV harnesses need — no sandcastle
# `--allow-local-binding` / `--allow-unix-sockets` escape hatch
# required. See clown ADR-0007 for the empirical sandbox survey.
[group("post-build")]
test-bats-net-cap:
  nix build .#bats-net_cap --no-link --print-build-logs

# Run bats integration tests against race-instrumented binaries.
# Catches data races that the unit-test -race pass won't, since several
# code paths only execute in the real CLI. Driven by `nix build
# .#bats-race`, a standalone derivation (`mkBatsLane`) that runs the
# bats suite against `madder-race`'s `$out/bin/madder`. net_cap-tagged
# scenarios are filtered out — the SFTP harness those tests need is
# a devshell-only derivation not exposed to nix-driven bats lanes.
[group("post-build")]
test-bats-race:
  nix build .#bats-race --print-build-logs --no-link

# Run bats integration tests against a coverage-instrumented binary.
# Driven by `nix build .#madder-cli-cover`, which builds the binary
# with `go build -cover`, runs the bats suite under a fresh GOCOVERDIR
# in the nix sandbox, then persists covdata fragments and a textfmt
# profile to `$out/`. `--no-link` skips creating a result symlink (we
# don't want to clobber `./result` and don't want a parallel
# `result-cli-cover` either); the store path comes back via
# `--print-out-paths` and we copy the artifacts we want into
# `.tmp/cover-data/` for the cover-merged / cover-summary recipes.
#
# net_cap-tagged scenarios are filtered out by the derivation — they
# need loopback binding the nix sandbox doesn't grant.
[group("post-build")]
test-bats-cover:
  #!/usr/bin/env bash
  set -euo pipefail
  out_path="$(nix build .#madder-cli-cover --no-link --print-out-paths --print-build-logs)"

  bats_dir="{{justfile_directory()}}/.tmp/cover-data/bats"
  rm -rf "$bats_dir"
  mkdir -p "$bats_dir"
  cp "$out_path"/covdata/* "$bats_dir"/
  chmod -R u+w "$bats_dir"

  out="{{justfile_directory()}}/.tmp/cover-data/bats-coverage.out"
  cp "$out_path/coverage.out" "$out"
  chmod u+w "$out"

  echo "==> Coverage written to $out (fragments at $bats_dir)"
  (cd go && go tool cover -func="$out" | tail -n 1)

# Merge unit-test and bats coverage into a combined profile at
# .tmp/cover-data/merged.out. Depends on test-go-cover and test-bats-cover
# having produced fragments under .tmp/cover-data/{unit,bats}/. Use this
# to see the full coverage picture across both lanes — anything still
# uncovered after both passes is a real gap.
[group("post-build")]
cover-merged: test-go-cover test-bats-cover
  #!/usr/bin/env bash
  set -euo pipefail
  cover_data="{{justfile_directory()}}/.tmp/cover-data"
  merged_dir="$cover_data/merged"
  out="$cover_data/merged.out"
  rm -rf "$merged_dir"
  mkdir -p "$merged_dir"
  cd go
  go tool covdata merge -i="$cover_data/unit,$cover_data/bats" -o="$merged_dir"
  go tool covdata textfmt -i="$merged_dir" -o="$out"
  echo "==> Merged coverage written to $out"
  go tool cover -func="$out" | tail -n 1

# Per-package coverage rollup with delta columns. Shows unit %, bats %,
# merged %, and bats-delta (how much bats adds beyond unit). Sorted
# ascending by merged % so the worst-covered packages surface first.
# Depends on cover-merged so all three profiles exist.
[group("post-build")]
cover-summary: cover-merged
  #!/usr/bin/env bash
  set -euo pipefail
  cover_data="{{justfile_directory()}}/.tmp/cover-data"
  awk_path="$cover_data/rollup.awk"
  cat > "$awk_path" <<'AWK_EOF'
  /^mode:/ { next }
  {
    split($1, a, ":")
    split(a[1], p, "/")
    pkg = ""
    for (i = 1; i < length(p); i++) pkg = pkg p[i] "/"
    sub(/\/$/, "", pkg)
    stmts = $(NF-1)
    count = $NF
    total[pkg] += stmts
    if (count + 0 > 0) covered[pkg] += stmts
  }
  END {
    for (k in total) {
      c = covered[k] + 0
      t = total[k]
      printf "%s\t%.1f\n", k, (t > 0 ? 100*c/t : 0)
    }
  }
  AWK_EOF
  unit_pct="$cover_data/by-package.unit"
  bats_pct="$cover_data/by-package.bats"
  merged_pct="$cover_data/by-package.merged"
  awk -f "$awk_path" "{{justfile_directory()}}/.tmp/go-cover.out" | sort > "$unit_pct"
  awk -f "$awk_path" "$cover_data/bats-coverage.out"             | sort > "$bats_pct"
  awk -f "$awk_path" "$cover_data/merged.out"                    | sort > "$merged_pct"
  printf '%-72s %7s %7s %8s %9s\n' "PACKAGE" "UNIT%" "BATS%" "MERGED%" "BATS_DELTA"
  printf '%-72s %7s %7s %8s %9s\n' "------------------------------------------------------------------------" "-----" "-----" "-------" "----------"
  join -t $'\t' -a 1 -a 2 -e '0.0' -o '0,1.2,2.2' "$unit_pct" "$bats_pct" \
    | join -t $'\t' -a 1 -a 2 -e '0.0' -o '0,1.2,1.3,2.2' - "$merged_pct" \
    | sort -t $'\t' -k4 -n \
    | awk -F $'\t' '{ printf "%-72s %6.1f%% %6.1f%% %7.1f%% %+9.1f\n", $1, $2, $3, $4, $3-$2 }'

# Run specific bats test files.
[group("post-build")]
test-bats-targets *targets: build
  MADDER_BIN={{justfile_directory()}}/result/bin/madder \
    HYPHENCE_BIN={{justfile_directory()}}/result/bin/hyphence \
    just zz-tests_bats/test-targets {{targets}}

# Run bats tests filtered by file_tag. Drives the auto-generated
# `.#bats-${tag}` flake output (one per `# bats file_tags=` directive
# discovered at flake-eval time). The lane runs under nix's build
# sandbox against the same `$out/bin/madder` `.#madder` produces, so
# dev-loop and release share one cache. `nix flake show` lists every
# available bats lane.
[group("post-build")]
test-bats-tags *tags:
  nix build --print-build-logs --no-link .#bats-{{tags}}

#   _____                          _
#  |  ___|__  _ __ _ __ ___   __ _| |_
#  | |_ / _ \| '__| '_ ` _ \ / _` | __|
#  |  _| (_) | |  | | | | | | (_| | |_
#  |_|  \___/|_|  |_| |_| |_|\__,_|\__|
#

# Format all source files via conformist (the treefmt successor): Go
# (goimports → gofumpt), Nix (nixfmt), shell/bats (shfmt). Config lives
# in ./conformist.toml. The read-only counterpart is `lint-fmt`.
[group("codemod")]
fmt:
  nix develop {{justfile_directory()}} --command conformist

#   _     _       _
#  | |   (_)_ __ | |_
#  | |   | | '_ \| __|
#  | |___| | | | | |_
#  |_____|_|_| |_|\__|
#

[group("pre-build")]
lint: lint-flake lint-fmt lint-facades

# Lint flake.lock for reducible input duplication (madder#214,
# doppelgang FDR-0002). Exits 1 on findings, so CI surfaces drift.
[group("pre-build")]
lint-flake:
  doppelgang lint --flake .

# Read-only format + lint gate via conformist (the treefmt successor).
# Verifies formatter drift (Go/Nix/shell, per ./conformist.toml) plus
# shellcheck. Runs inside the nix devshell so all formatter binaries
# (nixfmt, gofumpt, etc.) are on PATH. `just fmt` is the write mode.
[group("pre-build")]
lint-fmt:
  nix develop {{justfile_directory()}} --command conformist check

# Fail if the committed pkgs/ facades have drifted from their internal/
# sources. The nix build runs `dagnabit export` in preBuild
# (go/default.nix), so `just build` compiles against freshly generated
# facades and CANNOT catch committed drift — this is the only gate that
# does. Uses dagnabit's native, side-effect-free check (added in
# amarbel-llc/purse-first#123; the false-positive-on-a-correct-tree bug
# was fixed in #125): it renders a comparison copy into a dot-prefixed
# dir under the module root and exits nonzero if it differs from committed.
[group("pre-build")]
lint-facades:
  cd go && dagnabit export --check

#   __  __       _       _
#  |  \/  | __ _(_)_ __ | |_
#  | |\/| |/ _` | | '_ \| __|
#  | |  | | (_| | | | | | |_
#  |_|  |_|\__,_|_|_| |_|\__|
#

[group("maintenance")]
tidy:
  cd go && go mod tidy

# Update dewey to a version (e.g. just update-dewey v0.0.3).
[group("maintenance")]
update-dewey version:
  cd go && go get github.com/amarbel-llc/purse-first/libs/dewey@{{version}} && go mod tidy
  just gomod2nix

# Tag a Go module release. The "go/v" prefix is added for you, so pass
# the semver without it. Usage: just tag 0.0.1 "feat: public blob store API"
[group("maintenance")]
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

# Sed-rewrite MADDER_VERSION in version.env to the given semver.
# version.env is the single source of truth for the release version;
# flake.nix reads it via builtins.readFile, the bats version test
# reads it directly, and the binary picks it up via -ldflags injection
# (see go/internal/0/buildinfo). No-op if already at the target.
# Usage: just bump-version 0.0.2
[group("maintenance")]
bump-version new_version:
  #!/usr/bin/env bash
  set -euo pipefail
  current=$(grep '^MADDER_VERSION=' version.env | cut -d= -f2)
  if [[ "$current" == "{{new_version}}" ]]; then
    gum log --level info "already at {{new_version}}"
    exit 0
  fi
  sed -i.bak 's/^MADDER_VERSION=.*/MADDER_VERSION={{new_version}}/' version.env && rm version.env.bak
  gum log --level info "bumped MADDER_VERSION: $current → {{new_version}}"

# Cut a release: must be run on master. Bumps MADDER_VERSION in
# version.env, commits the bump with a changelog-style message built
# from commits since the last go/v* tag, pushes master, then signs
# and pushes the go/v{{version}} tag. The "go/v" prefix is added for
# you, so pass the semver without it. Usage: just release 0.0.2
#
# The `tag` recipe stays standalone for callers that want to control
# the commit message themselves without bumping. Release inlines the
# tag-step here because passing a multi-line message across `just`
# recipe boundaries was unreliable — the inner recipe saw a malformed
# argument and `git tag -s` would fail in a way that didn't surface
# until much later (see madder release-v0.3.0 incident).
[group("maintenance")]
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
  if ! git diff --quiet version.env; then
    git add version.env
    git commit -m "chore: release go/v{{version}}"
    git push origin master
    gum log --level info "pushed version.env bump to master"
  fi
  tag="go/v{{version}}"
  if [[ -n "$prev" ]]; then
    gum log --level info "Previous: $prev"
    git log --oneline "$prev"..HEAD -- go/ || true
  fi
  git tag -s -m "$msg" "$tag"
  gum log --level info "Created tag: $tag"
  git push origin "$tag"
  gum log --level info "Pushed $tag"

[group("maintenance")]
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

# Run `madder init -encryption <value> <store>` in an isolated tmp
# HOME/workdir to check whether an -encryption markl-id (or key path)
# parses and validates. Serves the encryption-key-vocabulary dev-loop:
# when madder gains a new format/purpose (e.g. piggy-* keys, madder#?),
# paste the markl-id `piggy list` emits as <value> and confirm it no
# longer fails with `unknown format id` / `IncompatiblePurposeAndFormat`.
# Note: parse/validate happens before any key material is touched, so a
# YubiKey need not be present; a recipient key still encrypts end-to-end
# only if its purpose permits it (auth/sig keys parse but are not
# encryption recipients). Same ceiling/tmp-home safety as
# debug-init-repro. Usage: just debug-init-encryption '<markl-id>' [storeid]
[group("debug")]
debug-init-encryption value storeid="enc":
  #!/usr/bin/env bash
  set -u
  root={{justfile_directory()}}
  home=$(mktemp -d)
  workdir=$(mktemp -d)
  cd "$workdir"
  unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME XDG_STATE_HOME
  export HOME="$home"
  export MADDER_CEILING_DIRECTORIES="$workdir"
  echo "=== init -encryption {{value}} {{storeid}} ==="
  set +e
  (cd "$root/go" && go run ./cmd/madder init -encryption "{{value}}" "{{storeid}}")
  rc=$?
  set -e
  echo "  exit=$rc"
  echo "(tmp workdir: $workdir)"
  echo "(tmp home:    $home)"

# Reproduce issue #145: `madder list` from a leaf below an inner
# `.madder/` should surface both ancestor `.madder/` stores with
# disambiguating dot prefixes. Builds a fresh fixture under tmp so it
# does not touch the host's $HOME or any real stores.
[group("debug")]
debug-issue-145-multi-ancestor-repro:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  just build >/dev/null
  madder_bin="{{justfile_directory()}}/result/bin/madder"

  home=$(mktemp -d)
  workdir=$(mktemp -d)
  trap 'rm -rf "$home" "$workdir"' EXIT
  cd "$workdir"

  unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME XDG_STATE_HOME XDG_LOG_HOME
  export HOME="$home"
  # Ceiling at workdir's parent so walk-up visits workdir and its
  # children but never leaks into the real $HOME.
  export MADDER_CEILING_DIRECTORIES="$(dirname "$workdir")"

  mkdir -p outer/inner/leaf
  cd outer
  "$madder_bin" init -encryption none .outer_only >/dev/null
  "$madder_bin" init -encryption none .default >/dev/null
  cd inner
  "$madder_bin" init -encryption none .default >/dev/null
  "$madder_bin" init -encryption none .inner_only >/dev/null
  cd leaf

  echo "=== madder list from $(pwd) ==="
  "$madder_bin" list

  echo
  echo "=== madder info-repo .default config-path (deepest) ==="
  "$madder_bin" info-repo .default config-path

  echo
  echo "=== madder info-repo ..default config-path (next ancestor up) ==="
  "$madder_bin" info-repo ..default config-path

# Exercise tap-dancer-backed TAP emitters and assert no `# Output` comment
# directives appear in the output. Catches regressions where a writer
# falls back to legacy bats-style `# Output: ...` lines instead of the
# tap-dancer YAMLish diagnostic block. Drives `madder write`
# (blob_write_sink) and `madder fsck` (blob_verify_sink) in -format tap
# mode against an isolated tmp home, so a real TTY is not required.
[group("debug")]
debug-tap-output:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}
  just build >/dev/null
  madder_bin="{{justfile_directory()}}/result/bin/madder"

  home=$(mktemp -d)
  workdir=$(mktemp -d)
  trap 'rm -rf "$home" "$workdir"' EXIT
  cd "$workdir"

  unset XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME XDG_STATE_HOME XDG_LOG_HOME
  export HOME="$home"
  export MADDER_CEILING_DIRECTORIES="$workdir"

  "$madder_bin" init -encryption none .default >/dev/null

  mkdir -p tree/sub
  echo alpha >tree/a.txt
  echo beta  >tree/b.txt
  echo gamma >tree/sub/c.txt

  combined=$(mktemp)
  trap 'rm -rf "$home" "$workdir" "$combined"' EXIT

  run_case() {
    local label="$1"; shift
    echo "=== $label ===" | tee -a "$combined"
    "$@" 2>&1 | tee -a "$combined"
    echo | tee -a "$combined"
  }

  run_case "madder write -format tap tree/a.txt" "$madder_bin" write -format tap tree/a.txt
  run_case "madder fsck -format tap .default" "$madder_bin" fsck -format tap .default

  echo "--- assertion: no '# Output' comment directive lines ---"
  if grep -nE '^[[:space:]]*# Output' "$combined"; then
    echo "FAIL: '# Output' directive appeared above" >&2
    exit 1
  fi
  echo "OK: '# Output' directive absent across $(wc -l <"$combined") lines"

# Smoke-test the madder MCP server's resources against the per-worktree
# spinclass `.default` blob store. Writes a known throwaway blob into
# .default, then drives `madder-mcp serve` over stdio with a sequence of
# JSON-RPC requests covering initialize, resources/list,
# resources/templates/list, and resources/read for madder://stores,
# madder://blobs (paginated), madder://stores/.default/blobs, and
# madder://blobs/<test-digest>. Each response is pretty-printed so the
# pagination and resource_link round-trips can be eyeballed. Requires
# `result/` to be populated (run `just build` first if it isn't).
[group("debug")]
debug-mcp-resources:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{justfile_directory()}}

  worktree="{{justfile_directory()}}"
  bin_madder="$worktree/result/bin/madder"
  bin_mcp="$worktree/result/bin/madder-mcp"

  for bin in "$bin_madder" "$bin_mcp"; do
    if [ ! -x "$bin" ]; then
      echo "missing $bin — run \`just build\` first" >&2
      exit 1
    fi
  done

  # Write a fresh throwaway blob to .default. The body includes a
  # timestamp so reruns produce distinct digests instead of resolving
  # to the same blob from a previous invocation.
  payload="madder MCP smoke test $(date -u +%Y-%m-%dT%H:%M:%SZ) $$"
  digest=$(printf '%s' "$payload" \
    | env MADDER_CEILING_DIRECTORIES="$worktree" \
        "$bin_madder" write -format json .default - \
    | head -n 1 \
    | jq -r '.id')
  echo "=== seeded blob ==="
  echo "  digest: $digest"
  echo "  body:   $payload"
  echo

  # Build the JSON-RPC request stream. Each line is one message; the
  # server processes them in order and exits when stdin closes. Each
  # request is built via jq -nc to dodge justfile interpolation and
  # quoting headaches.
  requests=$(mktemp)
  trap 'rm -f "$requests"' EXIT
  jq -nc '{jsonrpc:"2.0", id:1, method:"initialize", params:{protocolVersion:"2024-11-05", capabilities:{}, clientInfo:{name:"madder-mcp-smoke", version:"0"}}}' >>"$requests"
  jq -nc '{jsonrpc:"2.0", method:"notifications/initialized", params:{}}' >>"$requests"
  jq -nc '{jsonrpc:"2.0", id:2, method:"resources/list"}' >>"$requests"
  jq -nc '{jsonrpc:"2.0", id:3, method:"resources/templates/list"}' >>"$requests"
  jq -nc '{jsonrpc:"2.0", id:4, method:"resources/read", params:{uri:"madder://stores"}}' >>"$requests"
  jq -nc '{jsonrpc:"2.0", id:5, method:"resources/read", params:{uri:"madder://blobs?limit=5"}}' >>"$requests"
  jq -nc '{jsonrpc:"2.0", id:6, method:"resources/read", params:{uri:"madder://stores/.default/blobs?limit=5"}}' >>"$requests"
  jq -nc --arg uri "madder://blobs/$digest" '{jsonrpc:"2.0", id:7, method:"resources/read", params:{uri:$uri}}' >>"$requests"
  # Request 8 manufactures the openBlob → SFTP-panic → tryOpenInStore
  # skip path: this digest is intentionally absent from every store,
  # so openBlob walks default + remaining (which includes the
  # unreachable SFTP store). With step-2 of #134 the SFTP HasBlob
  # panic is converted to a per-store skip; without it, the panic
  # would escape openBlob and crash the server goroutine. Expected
  # response: a JSON-RPC error wrapping "blob not found" (or, if the
  # request-boundary recover fires, "...panicked..."), but in all
  # cases NOT a process crash.
  missing_digest="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"
  jq -nc --arg uri "madder://blobs/$missing_digest" '{jsonrpc:"2.0", id:8, method:"resources/read", params:{uri:$uri}}' >>"$requests"

  echo "=== requests ==="
  cat "$requests"
  echo

  responses=$(mktemp)
  stderr_log=$(mktemp)
  trap 'rm -f "$requests" "$responses" "$stderr_log"' EXIT

  # Capture stdout/stderr separately. madder-mcp can exit nonzero after
  # all responses have been emitted (e.g. when an SFTP store's
  # initializeOnce poisons the dewey context); tolerate that here so
  # the recipe exit code reflects whether the JSON-RPC responses
  # themselves are well-formed, not the server's shutdown status.
  set +e
  env MADDER_CEILING_DIRECTORIES="$worktree" "$bin_mcp" serve \
    <"$requests" >"$responses" 2>"$stderr_log"
  rc=$?
  set -e

  echo "=== responses ==="
  while IFS= read -r line; do
    printf '%s\n' "$line" | jq -C .
    echo
  done <"$responses"

  if [ -s "$stderr_log" ]; then
    echo "=== madder-mcp stderr (rc=$rc) ==="
    cat "$stderr_log"
  fi

  # Sanity-check that every request id received exactly one response.
  expected_ids="1 2 3 4 5 6 7 8"
  for id in $expected_ids; do
    if ! jq -e --argjson id "$id" 'select(.id == $id)' "$responses" >/dev/null; then
      echo "FAIL: no response with id=$id" >&2
      exit 1
    fi
  done
  echo "OK: received responses for ids $expected_ids"

# debug-serve-blob-api drives `madder serve`'s unix-socket HTTP blob API
# (GET/HEAD/PUT /blobs/<digest>) against the worktree's .default store —
# the integration smoke test for the circus admin daemon (FDR-0007).
# Requires `just build` first to populate result/.
[group("debug")]
debug-serve-blob-api:
  #!/usr/bin/env bash
  set -euo pipefail
  cd {{ justfile_directory() }}

  worktree="{{ justfile_directory() }}"
  bin_madder="$worktree/result/bin/madder"
  if [ ! -x "$bin_madder" ]; then
    echo "missing $bin_madder — run \`just build\` first" >&2
    exit 1
  fi

  run_madder() { env MADDER_CEILING_DIRECTORIES="$worktree" "$bin_madder" "$@"; }

  tmp=$(mktemp -d)
  sock="$tmp/madder.sock"
  srv_pid=""
  trap '[ -n "$srv_pid" ] && kill "$srv_pid" 2>/dev/null || true; rm -rf "$tmp"' EXIT

  # Seed a throwaway blob into .default (timestamped so reruns differ).
  payload="madder serve smoke $(date -u +%Y-%m-%dT%H:%M:%SZ) $$"
  digest=$(printf '%s' "$payload" | run_madder write -format json .default - | head -n 1 | jq -r '.id')
  echo "=== seeded blob ==="; echo "  digest: $digest"; echo

  # Start the daemon; wait for the socket to appear. Its stderr (store
  # discovery chatter — in a dev env that includes remote SFTP stores) is
  # captured to a log and only surfaced on failure.
  run_madder serve --socket "$sock" >"$tmp/serve.log" 2>&1 &
  srv_pid=$!
  for _ in $(seq 1 50); do [ -S "$sock" ] && break; sleep 0.1; done
  [ -S "$sock" ] || { echo "FAIL: socket never appeared (daemon crashed?)" >&2; cat "$tmp/serve.log" >&2; exit 1; }

  echo "=== HEAD seeded (expect 200) ==="
  code=$(curl -s -o /dev/null -w '%{http_code}' -I --unix-socket "$sock" "http://localhost/blobs/$digest")
  echo "  $code"; [ "$code" = "200" ] || { echo "FAIL: HEAD seeded = $code" >&2; exit 1; }

  echo "=== GET seeded (expect body match) ==="
  got=$(curl -s --unix-socket "$sock" "http://localhost/blobs/$digest")
  [ "$got" = "$payload" ] || { echo "FAIL: GET body mismatch: $got" >&2; exit 1; }
  echo "  body matches"

  echo "=== HEAD absent (expect 404) ==="
  missing="blake2b256-c5xgv9eyuv6g49mcwqks24gd3dh39w8220l0kl60qxt60rnt60lsc8fqv0"
  code=$(curl -s -o /dev/null -w '%{http_code}' -I --unix-socket "$sock" "http://localhost/blobs/$missing")
  echo "  $code"; [ "$code" = "404" ] || { echo "FAIL: HEAD absent = $code" >&2; exit 1; }

  echo "=== PUT then GET round-trip (expect 201, match) ==="
  newbody="put-roundtrip $(date -u +%Y-%m-%dT%H:%M:%SZ) $$"
  newdigest=$(printf '%s' "$newbody" | run_madder write -format json .default - | head -n 1 | jq -r '.id')
  code=$(printf '%s' "$newbody" | curl -s -o /dev/null -w '%{http_code}' -X PUT --data-binary @- --unix-socket "$sock" "http://localhost/blobs/$newdigest")
  echo "  PUT: $code"; [ "$code" = "201" ] || { echo "FAIL: PUT = $code" >&2; exit 1; }
  back=$(curl -s --unix-socket "$sock" "http://localhost/blobs/$newdigest")
  [ "$back" = "$newbody" ] || { echo "FAIL: PUT->GET mismatch: $back" >&2; exit 1; }
  echo "  round-trip matches"

  echo "=== PUT digest mismatch (expect 409) ==="
  code=$(printf 'unrelated bytes' | curl -s -o /dev/null -w '%{http_code}' -X PUT --data-binary @- --unix-socket "$sock" "http://localhost/blobs/$missing")
  echo "  $code"; [ "$code" = "409" ] || { echo "FAIL: mismatch PUT = $code" >&2; exit 1; }

  echo "=== --store .default: single-store backend serves that store by id ==="
  # Restart as a single-store daemon (--store) on a fresh socket and GET the
  # seeded blob through it — exercises makeBackend + storeBackend + the
  # open-by-id path. (.default is a writable cwd store; the //default system
  # path needs /var/lib/madder and is covered by the Go tests, not here.)
  kill "$srv_pid" 2>/dev/null || true; wait "$srv_pid" 2>/dev/null || true
  sock2="$tmp/madder-store.sock"
  run_madder serve --store .default --socket "$sock2" >"$tmp/serve-store.log" 2>&1 &
  srv_pid=$!
  for _ in $(seq 1 50); do [ -S "$sock2" ] && break; sleep 0.1; done
  [ -S "$sock2" ] || { echo "FAIL: --store socket never appeared" >&2; cat "$tmp/serve-store.log" >&2; exit 1; }
  got=$(curl -s --unix-socket "$sock2" "http://localhost/blobs/$digest")
  [ "$got" = "$payload" ] || { echo "FAIL: --store .default GET body mismatch: $got" >&2; exit 1; }
  echo "  --store .default serves the seeded blob"

  echo "OK: serve blob API (ambient + --store) GET/HEAD/PUT/404/409 all pass"
  # Stop the daemon and reap it so its SIGTERM exit code doesn't leak as
  # the recipe's status; the EXIT trap is the cleanup backstop.
  kill "$srv_pid" 2>/dev/null || true
  wait "$srv_pid" 2>/dev/null || true
  srv_pid=""
  exit 0
