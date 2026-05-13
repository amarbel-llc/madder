# pivy / piggy dependency scope

Date: 2026-05-13
Status: scope (research only — no migration plan yet)

## Why this doc exists

The user asked for a written scope of madder's coupling to "pivy" and
to `amarbel-llc/piggy`, the Rust port that is expected to take over
the same role on the madder side. Two facts shape the scope:

1.  There is **no standalone `github.com/amarbel-llc/pivy` repo in
    play**. The pivy surface madder consumes is the Go subpackage
    `github.com/amarbel-llc/purse-first/libs/dewey/delta/pivy`, which
    wraps the joyent/pivy `pivy-agent` daemon over its Unix socket.
    The only `go.mod` entry that brings the pivy code in is
    `github.com/amarbel-llc/purse-first/libs/dewey v0.1.0` at
    `go/go.mod:7`.
2.  **Piggy is currently Rust-only and downstream.** RFC 0002
    (`docs/rfcs/0002-markl-id-format.md:19-23, 305, 494, 522`) names
    `amarbel-llc/piggy` as the first non-Go markl implementation, a
    Rust port that consumes madder's wire format. It is not a Go
    library madder imports today; the user's longer-term intent is
    that piggy supplants `dewey/delta/pivy` as madder's pivy-agent
    bridge, so this doc also maps the migration surface so a future
    plan can pick it up without re-discovering everything.

## Current dependence surface (pivy, via dewey/delta/pivy)

### Go module wiring

-   `go/go.mod:7` — `require github.com/amarbel-llc/purse-first/libs/dewey v0.1.0`
    is the sole module that brings the pivy package into the build.
-   `go/go.sum:11-12` — pinned hash for dewey v0.1.0.

### Import sites (6 files)

| File | Used symbols | Role |
|---|---|---|
| `go/internal/bravo/markl/pivy_agent_discover.go:9, 66` | `pivy.CompressP256Point` | Discovers ECDH P-256 keys from `PIVY_AUTH_SOCK` / `SSH_AUTH_SOCK`, compresses each pubkey, registers it under `FormatIdPivyEcdhP256Pub`. |
| `go/internal/bravo/markl/format_family_pivyecdhp256.go:7, 15, 21, 27-29` | `pivy.DecompressP256Point`, `pivy.ResolveAgentSocketPath`, `pivy.IOWrapper{RecipientPubkey, DecryptECDH}`, `pivy.AgentECDHFunc` | Resolves a pivy-agent socket and builds the `IOWrapper` that decrypts blob ciphertext via the agent. |
| `go/internal/bravo/markl/format_family_ecdsap256.go:15, 145` | `pivy.CompressP256Point` | Compresses the candidate SSH-agent pubkey to find the matching `ecdsa-sha2-nistp256` signer in `SSH_AUTH_SOCK`. |
| `go/internal/foxtrot/blob_io/reader.go:13, 113` | `pivy.IsErrAgent` | Distinguishes "agent unreachable / PIN needed / card missing" from a generic decode failure so the reader does not silently fall back to unencrypted mode. |
| `go/internal/alfa/inventory_archive/data_writer_v1_test.go:15` | `pivy.IOWrapper`, `pivy.SoftwareECDHForTesting` | Software-only ECDH for round-trip tests (no real PIV card). |
| `go/internal/bravo/markl/format.go:27` | (constant only) `FormatIdPivyEcdhP256Pub = "pivy_ecdh_p256_pub"` | Wire-format ID for the recipient-key path. |

Grouped by capability the surface is small:

-   **Curve math:** `CompressP256Point`, `DecompressP256Point`.
-   **Agent IO:** `ResolveAgentSocketPath`, `AgentECDHFunc`,
    `IsErrAgent`, plus the `IOWrapper` struct shape
    (`RecipientPubkey`, `DecryptECDH`).
-   **Test helper:** `SoftwareECDHForTesting`.

That is the entire production surface piggy (or any replacement)
would have to cover for parity.

### Wire-format coupling (do not rename)

-   `pivy_ecdh_p256_pub` is a locked wire-format string per RFC 0002
    (`docs/rfcs/0002-markl-id-format.md:65, 234`) and the
    `dodder-*` locked-strings policy in `CLAUDE.md`. It appears in:
    -   `go/internal/bravo/markl/format.go:27` (the constant)
    -   `go/internal/charlie/markl_registrations/testdata/0002-markl-id-format-vectors.json:82-85`
        (cross-language conformance vectors)
    -   `go/internal/charlie/markl_registrations/rfc0002_conformance_test.go:22`
        (comment naming piggy as the Rust consumer of the same fixture)
    -   `docs/man.7/markl-id.md` (man-page table)
-   The format ID survives any swap of the underlying agent library;
    a piggy-based bridge must continue to emit and parse the same
    33-byte SEC-1 compressed P-256 encoding.

## Planned (not yet imported) pivy coupling

`docs/plans/2026-04-25-mmap-blob-encrypted-design.md:35, 71, 153-156,
230-254, 353-431` describes a future `go/internal/?/pivy_ebox_random/`
adapter that would parse joyent pivy's `ebox` container format for
mmap'd random-access decryption. It is gated on confirming that pivy
ebox is chunked-AEAD; the doc explicitly states "if pivy ebox is not
chunked-AEAD, this path supports age only and pivy stays excluded."
No code for this exists yet, so it does not add to today's surface
but it is the most likely future expansion of madder's pivy footprint
and any piggy-based bridge would need to grow into it as well.

## Piggy's current role

Piggy is referenced in three buckets, none of them as a Go import:

-   **RFC 0002 cross-language consumer** —
    `docs/rfcs/0002-markl-id-format.md:19-23, 305, 494, 522`. The RFC
    exists specifically so piggy (Rust) and madder (Go) cannot drift
    on the `pivy_ecdh_p256_pub` recipient-key encoding.
-   **Conformance-fixture audience** —
    `go/internal/charlie/markl_registrations/rfc0002_conformance_test.go:22`
    notes piggy loads the same vector file at
    `go/internal/charlie/markl_registrations/testdata/0002-markl-id-format-vectors.json`.
-   **Operator tooling, not a build dep** —
    `docs/plans/2026-05-10-cutting-garden-framework-bootstrap.md:28, 48,
    51` mention `pivy-agent` as a precondition for GPG-signed commits.
    That is a developer-environment expectation, not a code-level
    coupling.

## Migration surface (pivy → piggy)

Because piggy is Rust and madder is Go, "piggy replaces
dewey/delta/pivy" cannot be a straight import swap. The realistic
shapes are:

1.  Piggy ships a `pivy-agent`-compatible socket protocol and madder
    keeps talking over the existing socket — `dewey/delta/pivy` is
    either re-pointed at piggy's implementation or rewritten in
    madder/dewey as a pure Go client.
2.  Piggy exposes a CLI / library bridge (cgo, gRPC, stdio JSON) that
    a new madder subpackage adapts behind the same exported symbol
    set listed above.

Either way the **stable contract** madder needs is the six-symbol
surface in *Import sites* plus the wire-format ID
`pivy_ecdh_p256_pub` and its 33-byte SEC-1 encoding.

Open questions to resolve before a migration plan can be drafted:

-   How does piggy expose itself to Go callers (socket, FFI, CLI)?
-   Will the `PIVY_AUTH_SOCK` / `SSH_AUTH_SOCK` environment contract
    continue, or will piggy define its own socket env var?
-   Does piggy own the future `pivy_ebox_random` adapter from the
    2026-04-25 design, or does that stay on the Go side regardless?
-   Is `SoftwareECDHForTesting` (currently consumed only by
    `data_writer_v1_test.go`) something piggy intends to ship, or
    does madder need its own software-ECDH path for tests after the
    migration?

These are flagged here so a follow-up migration plan can answer them
in one place.

## How to verify this scope is still accurate

-   `git grep -n 'dewey/delta/pivy' go/` should return exactly the
    five *importing* rows of the *Import sites* table above. The
    sixth row, `go/internal/bravo/markl/format.go`, only defines the
    `FormatIdPivyEcdhP256Pub` constant and does not import the pivy
    package, so it does not appear in this grep. If new files appear,
    update the table.
-   `git grep -nE 'pivy|piggy' docs/` should match only the documents
    cited under *Current dependence surface*, *Planned pivy coupling*,
    and *Piggy's current role* (plus this file itself). New hits
    indicate either a new prose reference to triage or genuine new
    coupling.
-   `grep -n 'amarbel-llc/pivy\|amarbel-llc/piggy' go/go.mod go/go.sum`
    should return nothing; the day a hit appears here is the day
    madder gains a direct piggy/pivy module dependency and this scope
    needs to be replaced by a migration plan.
