---
status: proposed
date: 2026-04-24
---

# Test-Subprocess Handshake Protocol

## Abstract

This document specifies a subprocess handshake protocol for in-process test harnesses that need to spawn long-lived network-service helpers (for example: a throwaway SFTP server bound to a loopback port). The protocol defines a magic-cookie envelope carried via an environment variable, a single-line pipe-delimited handshake record emitted on the child's standard output, and a stdin-close-as-shutdown lifecycle contract. The design is adapted from the pattern popularized by Hashicorp's `go-plugin` library but deliberately limits itself to process bring-up — the specified subprocess serves its own protocol (SSH, SFTP, HTTP, etc.) over the advertised address, not RPC defined by this document.

## Introduction

Madder's bats test suite runs under sandcastle (via batman), which applies a default-deny network-capability profile. Adding a network-service test harness — such as `madder-test-sftp-server` for [issue #54](https://github.com/amarbel-llc/madder/issues/54) — requires the test runner to spawn the helper, discover the ephemeral port the helper bound, and coordinate graceful shutdown when the test completes. The same pattern will apply to future harnesses for HTTP services, CalDAV servers, or any other network endpoint madder learns to talk to.

Dodder's `test-sftp-server` (the prior art being ported in #54) uses a simpler space-delimited `READY port=NNNN known_hosts=PATH` line. That simpler scheme has proven fragile under sandboxing: when the READY line fails to arrive, tests hang silently with no diagnostic ([dodder#118](https://github.com/amarbel-llc/dodder/issues/118)). This RFC specifies a more robust variant that:

1. Uses a magic-cookie envelope to detect stdout pollution (a child process emitting unrelated data on stdout before the handshake).
2. Advertises a protocol version so future changes are explicit rather than silently breaking.
3. Defines a deterministic shutdown contract (stdin-close → graceful exit) so parent termination never orphans the child.

This RFC scopes only the handshake and lifecycle. The application protocol spoken over the advertised address (SSH/SFTP for `madder-test-sftp-server`, HTTP for future HTTP harnesses, etc.) is out of scope — the child implements whatever protocol is useful to the test.

## Requirements Language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## Specification

### Roles

- **Parent**: the test runner (typically a bats script invoking the child via a `coproc`-style pipe) that spawns the child and consumes its handshake.
- **Child**: the subprocess binary (e.g., `madder-test-sftp-server`) that implements this protocol and serves its application protocol on the advertised address.

### Cookie Envelope

The parent MUST generate a fresh cookie per invocation. The cookie SHOULD be derived from at least 128 bits of cryptographically-strong random entropy (e.g., `head -c 16 /dev/urandom | xxd -p`).

The parent MUST pass the cookie to the child via the environment variable `MADDER_PLUGIN_COOKIE`. The cookie MUST NOT be passed on the command line.

The child MUST read `MADDER_PLUGIN_COOKIE` from its environment before producing any other output. If the variable is unset, empty, or does not match the cookie the child was built with (when the child embeds an expected cookie — see Cookie Embedding below), the child MUST:

1. Print a single line to standard error beginning with `[<name>] magic cookie mismatch` (where `<name>` is the child's program name).
2. Exit with status `1`.
3. Produce no output on standard output.

The cookie serves as a mutual-authentication signal: it proves to the child that its parent knows the secret, and it proves to the parent's handshake parser that the line on stdout originated from the intended child (see Handshake Line below).

#### Cookie Embedding

A child MAY embed a build-time-constant expected cookie against which the runtime `MADDER_PLUGIN_COOKIE` is compared. This is OPTIONAL and MAY be omitted when the child's only realistic caller is a specific known parent. When used, the embedded cookie MUST be a compile-time constant (not a runtime value), and the parent MUST know the same constant.

### Handshake Line

After cookie validation succeeds and the child has bound its application-protocol listener, the child MUST print exactly one line to standard output with the following structure:

```
<cookie>|<protocol_version>|<transport>|<address>|<subprotocol_metadata>|<subprotocol>
```

All fields are separated by the ASCII pipe character `|` (U+007C, 0x7C). The line MUST terminate with `\n` (U+000A, 0x0A). No additional data MAY appear on standard output before this line.

| Field | Type | Description |
|---|---|---|
| `cookie` | String, printable ASCII, no whitespace, no `\|` | The exact cookie value from `MADDER_PLUGIN_COOKIE`. |
| `protocol_version` | Decimal integer | This document specifies version `1`. |
| `transport` | String, lowercase | `tcp`, `tcp4`, `tcp6`, or `unix`. |
| `address` | String | For TCP: `HOST:PORT` (e.g., `127.0.0.1:45678`). For Unix: absolute filesystem path. |
| `subprotocol_metadata` | String, no `\|` | Opaque to this protocol; carries subprotocol-specific handshake data as `key=value` (see below for SFTP example). MAY be empty. |
| `subprotocol` | String, lowercase | `ssh`, `http`, `https`, `caldav`, etc. Signals what protocol the parent MUST speak to the advertised address. |

Any additional data on standard output MUST follow this line. A child that produces no further output after the handshake SHOULD close its stdout writer (optional; useful to signal end-of-headers).

Example valid handshake line for `madder-test-sftp-server`:

```
a1b2c3d4e5f6789012345678|1|tcp|127.0.0.1:45678|known_hosts=/tmp/mktemp.XXXX/known_hosts|ssh
```

### Subprotocol Metadata

The `subprotocol_metadata` field MUST be a sequence of zero or more `key=value` pairs separated by `&` (ASCII 0x26). Keys MUST be lowercase ASCII alphanumeric-plus-underscore; values MUST NOT contain `|`, `&`, or unescaped whitespace. If the subprotocol has no metadata to advertise, the field MUST be empty (between the two surrounding `|` delimiters).

For `subprotocol=ssh` children, the following keys are RESERVED:

- `known_hosts` — REQUIRED. Absolute path to a file in OpenSSH `known_hosts` format containing the child's host public key. The file MUST exist and be readable by the parent when the handshake line is emitted.

### Lifecycle

After the child prints the handshake line, it MUST:

1. Begin accepting connections on the advertised `address` via the advertised `transport`.
2. Speak the advertised `subprotocol` to connected clients.
3. Continue reading from its standard input. A closed stdin (EOF) is the sole normative shutdown signal.

The parent MUST NOT close the child's standard input until all tests that depend on the child have completed. When the parent closes the child's standard input, the child MUST:

1. Stop accepting new connections.
2. Allow in-flight connections up to a grace period (RECOMMENDED: 5 seconds) to complete.
3. Release any resources it advertised in `subprotocol_metadata` (e.g., delete the `known_hosts` file for SSH children).
4. Exit with status `0` if shutdown completed cleanly, or `1` if any cleanup step failed.

The child MUST also honor SIGTERM as a shutdown signal with identical semantics to stdin-close.

Children SHOULD additionally run a watchdog that exits with status `1` after a configurable "no-activity" period (RECOMMENDED default: 30 seconds) if no stdin data arrives and no connections are established. This prevents orphaned listeners when the parent dies without reaching normative shutdown. Watchdog activity MAY be reset by either stdin reads or incoming connections.

### Error Signaling

Any error that prevents successful handshake MUST result in:

1. A single line on standard error beginning with `[<name>] <error description>`.
2. A non-zero exit status.
3. No handshake line on standard output.

After a successful handshake, errors in serving the subprotocol MUST be reported via standard error only; the child MUST NOT print additional handshake-like lines on standard output.

## Security Considerations

**Cookie scope.** The cookie is a short-lived secret whose only role is to prove that the parent invoking the child knows the value. It is NOT suitable as a general authentication token for the subprotocol itself. In particular:

- The cookie appears in the environment of the child, which may be readable by other processes owned by the same user on some systems. Parents MUST NOT reuse a cookie across invocations.
- The cookie is not used to authenticate subprotocol connections. Test harnesses relying on this protocol MUST ensure their application-protocol traffic is either unauthenticated (acceptable for loopback-only test servers) or uses its own authentication mechanism (e.g., SSH host keys, bearer tokens).

**Loopback binding.** The RECOMMENDED transport is loopback-only (`127.0.0.1` or `::1`). Children MUST NOT bind to `0.0.0.0` or a routable interface. Running a test-only service on a routable interface exposes the subprotocol to the network, which is almost always wrong.

**`known_hosts` cleanup.** When a child advertises `known_hosts=<path>`, the path MUST be in a directory that is either (a) owned exclusively by the test-run user or (b) cleaned up by the parent's test framework. Writing `known_hosts` files to shared locations creates a denial-of-service vector for other processes on the machine that happen to use SSH to the same host:port pair.

**Stdout as a capability channel.** The handshake line discloses the advertised address, which is typically sensitive (a running SSH server, however ephemeral, is still a service). Parents MUST NOT log the handshake line to shared output destinations without redaction. The cookie field of the handshake line is less sensitive (it's already in the child's environment) but parents SHOULD still redact it when logging to shared destinations.

**Trust boundaries.** This protocol is designed for in-process parent-child communication between a test runner and a subordinate binary the test runner compiled or installed itself. It MUST NOT be used to communicate with subprocesses supplied by untrusted users — the cookie envelope does not defend against malicious children (a malicious child can simply lie in its handshake line).

## Conformance Testing

Conformance tests for this specification live alongside the binary that implements it. For the reference implementation `madder-test-sftp-server` shipped in #54, tests live in `go/cmd/madder-test-sftp-server/` (unit tests) and `zz-tests_bats/sftp.bats` (integration tests).

Tests MUST use `bats-emo`'s `require_bin` for binary injection; hard-coded build output paths are forbidden:

```bash
# correct
require_bin MADDER_TEST_SFTP_SERVER madder-test-sftp-server
```

### Covered Requirements (for the reference implementation)

| Requirement | Test Location | Description |
|---|---|---|
| Cookie Envelope, MUST exit 1 on cookie mismatch | `go/cmd/madder-test-sftp-server/main_test.go` | Table test invoking the binary with no / wrong cookie. |
| Handshake Line, MUST print exactly one line matching the schema | `go/cmd/madder-test-sftp-server/main_test.go` | Unit test capturing stdout, parsing the line, asserting field-by-field. |
| Lifecycle, MUST exit 0 on stdin close | `go/cmd/madder-test-sftp-server/main_test.go` | Test that spawns the binary, asserts handshake, closes stdin, waits for clean exit. |
| Integration: full handshake + SFTP publish + audit record | `zz-tests_bats/sftp.bats` | Observer-focused scenarios from #54 — the handshake is implicitly validated by the fact that madder can connect. |

Future binaries implementing this protocol for other subprotocols (HTTP harness, CalDAV harness, etc.) MUST add equivalent coverage of the normative requirements above in their own test files.

## Compatibility

This document specifies protocol version `1`. Any backwards-incompatible change to the handshake line format or lifecycle semantics MUST increment the `protocol_version` field. Parents and children SHOULD refuse to proceed when their understood protocol versions do not match.

Additive changes — new reserved `subprotocol_metadata` keys, new transports, new subprotocols — MAY be made within version `1` as long as parents that do not recognize them can still parse the handshake line. Unknown `subprotocol_metadata` keys MUST be ignored by parents that do not understand them.

The simpler READY-line format used by dodder's upstream `test-sftp-server` (`READY port=NNNN known_hosts=PATH`) is NOT compatible with this protocol. Children ported from dodder (see #54) MUST be updated to emit the handshake specified here; the dodder variant is not permitted.

## References

### Normative

- **RFC 2119** — Bradner, S., "Key words for use in RFCs to Indicate Requirement Levels", BCP 14, RFC 2119, March 1997.
- **RFC 4251** — Ylonen, T. and C. Lonvick, "The Secure Shell (SSH) Protocol Architecture", RFC 4251, January 2006. (Subprotocol for `subprotocol=ssh` children.)

### Informative

- `docs/plans/2026-04-24-sftp-test-harness-design.md` — the design document for #54 that motivates this RFC.
- [Hashicorp go-plugin](https://github.com/hashicorp/go-plugin) — the pattern-level inspiration for this protocol. go-plugin handles RPC transport on top of a similar bring-up handshake; this RFC scopes only the bring-up.
- [dodder#118](https://github.com/amarbel-llc/dodder/issues/118) — the upstream silent-handshake-failure bug that motivates the more-robust envelope and diagnostics specified here.
- madder issue #54 — first consumer of this protocol (`madder-test-sftp-server`).
- madder issue #55 — planned expansion of SFTP test coverage that will exercise more of this protocol's lifecycle surface.
