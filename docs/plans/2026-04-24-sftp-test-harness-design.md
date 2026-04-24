# SFTP bats test harness — design

**Status:** approved
**Date:** 2026-04-24
**Issues:** #54 (this work), #55 (expanded coverage follow-up), #50 (SFTP observer wiring that this harness proves out)

## Context

#44 landed the per-blob write-log observer; #50 wired it into the remote-SFTP publish path. Neither PR shipped an integration test because madder has no SFTP test infrastructure today. The prior art — dodder's `test-sftp-server` + `sftp.bats` — is `skip`ped upstream due to [dodder#118](https://github.com/amarbel-llc/dodder/issues/118): `test-sftp-server` binds on `127.0.0.1` and the sandcastle sandbox madder uses (via batman) denies loopback bind by default.

This design ports dodder's harness into madder with two upgrades over the upstream version:

1. A homegrown hashicorp-plugin-inspired handshake protocol (magic cookie, protocol version, pipe-delimited fields, stdin-close-as-shutdown) so future silent handshake failures surface loudly.
2. Integration with batman's `--allow-local-binding` flag (which flips sandcastle's `allowLocalBinding: true` at config-generation time) via a dedicated `net_cap`-tagged bats partition.

## Architecture (5 components)

1. **`go/cmd/madder-test-sftp-server/main.go`** — ~200 lines, ported from `~/eng/repos/dodder/go/cmd/test-sftp-server/main.go` and augmented with the handshake protocol (see RFC 0001). Embedded SSH/SFTP server on `crypto/ssh` + `github.com/pkg/sftp`, ECDSA host key, `127.0.0.1:0` listener.

2. **Nix derivation in `go/default.nix`** — local `buildGoModule` for `madder-test-sftp-server`. Added to `devShells.default.buildInputs` in `flake.nix`. **Never surfaced in `flake.packages` or `flake.apps`** — devshell-only per user request. `madder-test-sftp-server` is on `PATH` inside `nix develop` / direnv.

3. **`zz-tests_bats/lib/sftp.bash`** — helper file loaded only by `sftp.bats`. Exports:
   - `start_sftp_server` — generates random cookie, execs `madder-test-sftp-server` as a coprocess with `MADDER_PLUGIN_COOKIE=<cookie>`, reads the handshake line with a 5s timeout, parses port + known_hosts path, exports `SFTP_PORT` / `SFTP_KNOWN_HOSTS` / `SFTP_PID`.
   - `stop_sftp_server` — closes the coprocess's stdin (signals graceful shutdown), waits for the PID.

4. **`zz-tests_bats/sftp.bats`** — file-tagged `# bats file_tags=net_cap`. Observer-focused scope (#54):
   - `sftp_write_emits_written_record` — init SFTP store pointing at `sftp://localhost:$SFTP_PORT/...`, write a blob, assert one `op:"written"` record under `$XDG_LOG_HOME/madder/`.
   - `sftp_write_disabled_by_no_write_log_flag` — `--no-write-log` → no log file for SFTP path.
   - `sftp_write_disabled_by_env_var` — `MADDER_WRITE_LOG=0` → same.
   - `sftp_write_record_has_contracted_fields` — every ADR-0004 contracted field present in the SFTP record.

5. **Justfile recipe partition** — existing `test-bats` becomes `bats --filter-tags '!net_cap' *.bats` (default-deny sandcastle, excludes net-cap tests). New `test-bats-net-cap` runs `bats --allow-local-binding --filter-tags net_cap *.bats`. Default `just test` composition gains the new recipe.

## Data flow

See RFC 0001 for the full handshake protocol. The short version:

```
bats → exec madder-test-sftp-server (MADDER_PLUGIN_COOKIE=<rand>)
     ← "MADDER_SFTP|1|tcp|127.0.0.1:NNNN|known_hosts=/tmp/.../kh|ssh"
     → (tests run, SFTP traffic on the advertised port)
     → close stdin (signals shutdown)
     ← EOF, graceful cleanup, exit 0
```

## Error handling

- Handshake read times out at 5s; helper captures server stderr, calls `fail` with the captured output. Converts #118-style silent hangs to loud diagnostics.
- Cookie mismatch: server exits 1 with `[test-sftp-server] magic cookie mismatch` on stderr. Prevents accidental cross-test invocation.
- Parent (bats) dies: server's stdin reaches EOF; server detects and exits cleanly. Watchdog exits after 30s of no-stdin-activity as a safety net.
- Port-in-use race: impossible; `:0` picks ephemeral.
- `known_hosts` cleanup: deleted on graceful exit. Bats tempdir cleanup is the fallback.

## Testing strategy

- **Go unit tests for `madder-test-sftp-server`** — table test for handshake-line format. Runs under default sandcastle (no network binding in the unit test itself).
- **The 4 `sftp.bats` scenarios** above.
- **Full `just test` composition** stays green on both partitions.

## Rollback

Purely additive — no existing infrastructure is replaced.

- **Dual-architecture period**: n/a. Nothing pre-existed.
- **Promotion criterion**: 7 days of green CI once #55 (expanded parity coverage) lands.
- **Rollback procedure**: remove the new `test-bats-net-cap` recipe from `just test`'s composition. One-line revert. Pre-existing tests continue untouched. The `# bats file_tags=net_cap` tag ensures the sftp.bats file is filtered out in the default `test-bats` recipe with no further change.

## References

- RFC 0001 — test-subprocess handshake protocol (captures the reusable pattern for future network-service test harnesses).
- ADR 0004 — the audit-log spec this harness proves out end-to-end.
- #54, #55, #50 — related issues.
- dodder #118 — upstream sandbox blocker that motivates the sandcastle-aware test partition.
