# Hyphence utility — design

**Status:** proposed 2026-05-03

**Date:** 2026-05-03

**Tracks:** new utility (no parent issue)

**Related:**
- Format spec: [`docs/rfcs/0001-hyphence.md`](../rfcs/0001-hyphence.md), [`docs/man.7/hyphence.md`](../man.7/hyphence.md)
- Existing convergence work: [#115](https://github.com/amarbel-llc/madder/issues/115) (madder owns the canonical hyphence parser)
- Follow-ups filed alongside this design: [#126](https://github.com/amarbel-llc/madder/issues/126), [#127](https://github.com/amarbel-llc/madder/issues/127), [#128](https://github.com/amarbel-llc/madder/issues/128), [#129](https://github.com/amarbel-llc/madder/issues/129), [#130](https://github.com/amarbel-llc/madder/issues/130)

## Goals

- Ship a new sibling utility `hyphence` (binary at `go/cmd/hyphence/`) for **format-only** inspection and re-emission of on-disk hyphence documents. Subcommands: `validate`, `meta`, `body`, `format`.
- Operate purely on the RFC 0001 wire format. No coupling to madder's type-coder registry, no decoding of `! type-string` payloads, no blob-store integration.
- Reuse the existing `hyphence.Reader` streaming engine (single-pass, both halves stream) — extend it with a small set of `io.ReaderFrom` consumers rather than introducing a parallel parser.
- Take inspiration from dodder's `object_metadata_fmt_hyphence` (`MetadataOnly`/`BlobOnly` mode concepts; the inline-body-AND-`@` rejection rule). Do **not** port dodder code — its `Formatter`/`Parser` interfaces are welded to dodder's domain model.

## Non-goals

- Type-aware decoding. The user explicitly rejected the "git cat-file for hyphence" framing. A future `hyphence query` subcommand that pretty-prints body content per `!`-line type-tag is out of scope; tracked indirectly via [#128](https://github.com/amarbel-llc/madder/issues/128) (structured lock parsing).
- Blob-store integration. `hyphence` accepts file paths and stdin only — never blob-store-ids.
- Multi-document handling. Every known producer writes one document per file; no concatenated-document support in v1.
- Extracting the document model to a standalone repo or to `pkgs/hyphence_document/`. Per `~/eng/CLAUDE.md` convergence rule, defer until a third concrete consumer appears.
- Aggregated multi-error validation, structured lock parsing, canonical-line-order tie-breaking spec, cross-utility integration tests — each filed as a follow-up issue.

## CLI surface

```
hyphence                                    Print help, exit 64 (EX_USAGE).
hyphence <subcommand>                       Subcommand without input — print help, exit 64.
hyphence validate <path|->                  Strict RFC 0001 check. Exit 0 silent on pass.
                                            Exit 65 (EX_DATAERR) on fail; one line-numbered
                                            diagnostic on stderr.
hyphence meta <path|->                      Print metadata section verbatim, prefixes intact,
                                            without the surrounding `---` boundaries. Exit 0.
hyphence body <path|->                      Print body section verbatim. If the document has
                                            no body, print nothing and exit 0.
hyphence format <path|->                    Re-emit canonicalized per RFC §Canonical Line
                                            Order. Existing comments stay anchored to their
                                            following non-comment line. Exit 0.
```

**Common rules:**

- Single positional arg per subcommand: a file path, or `-` for stdin. No glob/recursion; shells handle that.
- All output to stdout. Errors and diagnostics to stderr.
- Global flag `--no-inventory-log` from `futility.Utility` stays mounted (no-op for `hyphence` since it performs no blob writes) — keeps the global flag set consistent across the four utilities.
- No type-aware behavior anywhere. Body bytes are opaque.

**Exit codes** (sysexits-style):

- `0` — success
- `64` (`EX_USAGE`) — bad arguments / missing required positional
- `65` (`EX_DATAERR`) — input is not valid hyphence per RFC 0001
- `66` (`EX_NOINPUT`) — file not found or unreadable
- `74` (`EX_IOERR`) — read/write error after parse started

**Examples** (will appear in `hyphence(1)`):

```
$ hyphence validate ~/.local/log/madder/inventory_log/2026-05-03/01HW…hyphence
$ hyphence meta receipt.hyphence | grep '^!'
! cutting_garden-capture_receipt-fs-v1
$ hyphence body receipt.hyphence | jq -r '.entry_path'
$ hyphence format old.hyphence > canonical.hyphence
```

## Architecture & package layout

```
go/internal/charlie/hyphence/         ← existing package, gains:
    document.go         // Document, MetadataLine, sentinel errors
    canonicalize.go     // Canonicalize(*Document)
    consumers.go        // MetadataStreamer, MetadataBuilder, MetadataValidator,
                        //   FormatBodyEmitter — all io.ReaderFrom
    document_test.go
    consumers_test.go
    rfc_validate_test.go         // extends testdata/rfc_vectors.txt harness
    (existing: main.go w/ //go:generate dagnabit export, coder.go, reader.go, etc.)

go/pkgs/hyphence/main.go              ← auto-regenerated by `just generate-facades`

go/internal/india/commands_hyphence/  ← new utility (no dagnabit; CLI-only)
    main.go             // futility.NewUtility("hyphence", ...) + Examples/EnvVars/Files
    globals.go          // Globals struct (currently just NoInventoryLog)
    validate.go         // utility.AddCmd("validate", &Validate{})
    meta.go             // utility.AddCmd("meta", &Meta{})
    body.go             // utility.AddCmd("body", &Body{})
    format.go           // utility.AddCmd("format", &Format{})
    CLAUDE.md

go/cmd/hyphence/main.go               ← thin entrypoint, mirrors madder-cache/main.go

docs/man.1/hyphence-{validate,meta,body,format}.md
docs/man.1/hyphence.md                ← top-level utility man page

zz-tests_bats/hyphence.bats           ← integration tests
```

**Why one internal package** (no nested sub-package): no precedent in the tree for nested dagnabit; existing `//go:generate dagnabit export` on `hyphence/main.go` picks up new exports automatically; conceptual separation between format-only and type-aware code is handled via filenames within the package.

**What's lifted from dodder**: the `MetadataOnly`/`BlobOnly` mode *concepts* (now baked into our `Meta`/`Body` subcommands), and the inline-body-AND-`@` rejection rule (RFC 0001 §Metadata Lines, dodder's `ErrHasInlineBlobAndFilePath`). Not the dodder code — its parser is welded to `objects.EncoderContext`/`BlobWriterFactory`/`fields.Field`/`script_config.RemoteScript`.

## Data flow & parsing model

Single-pass streaming via the existing `hyphence.Reader{Metadata, Blob}` engine. Both halves stream; only consumers that need structure pay for buffering.

**New types in `go/internal/charlie/hyphence/`:**

```go
// Document is the parsed metadata section of a hyphence document.
// The body is never buffered into Document; it's streamed by Blob consumers.
type Document struct {
    Metadata          []MetadataLine  // source order
    TrailingComments  []string        // % lines after the last non-comment line
    HasBody           bool            // true iff a body separator + body section followed
}

// MetadataLine is a single per-prefix line.
type MetadataLine struct {
    Prefix          byte         // one of '!', '@', '#', '-', '<', '%'
    Value           string       // bytes after "<prefix> ", trailing newline stripped
    LeadingComments []string     // % lines that preceded this line in source order
}

var (
    ErrMalformedMetadataLine     = errors.New("malformed metadata line")
    ErrInvalidPrefix             = errors.New("invalid metadata prefix")
    ErrInlineBodyWithAtReference = errors.New("blob reference '@' line forbidden when body section follows")
)

// MetadataStreamer copies metadata bytes verbatim to W. Used by `hyphence meta`.
type MetadataStreamer struct { W io.Writer }
func (m *MetadataStreamer) ReadFrom(r io.Reader) (int64, error)

// MetadataBuilder buffers metadata lines into Doc as structured MetadataLines.
// Used by `hyphence format`.
type MetadataBuilder struct { Doc *Document }
func (m *MetadataBuilder) ReadFrom(r io.Reader) (int64, error)

// MetadataValidator validates each metadata line against RFC 0001 §Metadata Lines.
// Reports first violation as an error with line number.
// Tracks SawAtLine for the cross-line check in the validate subcommand.
type MetadataValidator struct {
    SawAtLine bool
    line      int  // 1-based, internal
}
func (m *MetadataValidator) ReadFrom(r io.Reader) (int64, error)

// FormatBodyEmitter is the Blob consumer for `hyphence format`. By the time
// ReadFrom fires, MetadataBuilder has populated Doc; this emits the
// canonicalized metadata section then streams the body straight through.
type FormatBodyEmitter struct {
    Doc *Document
    Out io.Writer
}
func (f *FormatBodyEmitter) ReadFrom(r io.Reader) (int64, error)

// Canonicalize sorts doc.Metadata in place per RFC §Canonical Line Order.
// Stable sort within each prefix preserves insertion order. Each line drags
// its LeadingComments. TrailingComments stay last.
func Canonicalize(doc *Document)
```

**Per-subcommand wiring** — all use `Reader{RequireMetadata: true, AllowMissingSeparator: false}`:

| Subcommand | Metadata consumer | Blob consumer | After ReadFrom |
|------------|-------------------|---------------|----------------|
| `validate` | `&MetadataValidator{}` | drain to `io.Discard`, count bytes | run cross-check: if `validator.SawAtLine && bodyDrain.SawBody` → `ErrInlineBodyWithAtReference` |
| `meta`     | `&MetadataStreamer{W: stdout}` | drain to `io.Discard` | — |
| `body`     | drain to `io.Discard` | stdout writer | — |
| `format`   | `&MetadataBuilder{Doc: &doc}` | `&FormatBodyEmitter{Doc: &doc, Out: stdout}` | — |

**Boundary-level strictness is already in `hyphence.Reader`.** Equality testing against the literal `"---"` after `TrimSuffix("\n")` already rejects trailing whitespace, extra hyphens, and `\r`-before-`\n`. The only knob to flip is leaving `AllowMissingSeparator: false` (default). Per-line strictness is the metadata consumer's job — `MetadataValidator` covers it.

**`meta`'s deliberate looseness.** `MetadataStreamer` does not run RFC line checks; a malformed prefix in input gets printed through. Boundary-level errors *do* still surface (boundary scanner runs regardless of consumer). Users who want strict per-line checks before meta-stripping run `hyphence validate` first.

## Error handling & diagnostics

**Diagnostic format** — gcc-style, single line, all errors:

```
hyphence: <subcommand>: <source>:<line>: <reason>
```

`<source>` is the file path or `-` for stdin. `<line>` is 1-based, threaded from boundary scanner through metadata consumer. No offending-line snippet in v1 (deferred polish).

Examples:

```
hyphence: validate: receipt.hyphence:5: invalid metadata prefix 'X' (must be one of !@#-<%)
hyphence: validate: -:7: missing blank line after closing boundary
hyphence: validate: log.hyphence:3: blob reference '@' line forbidden when body section follows
hyphence: meta: receipt.hyphence:1: expected "---" but got "hello"
```

**Validate stops on first failure.** Aggregating multi-error reporting requires recovery state; see [#127](https://github.com/amarbel-llc/madder/issues/127).

**Cross-line check** runs after `Reader.ReadFrom` returns. `MetadataValidator.SawAtLine` is set when an `@` line passes per-line checks; the body drain `io.ReaderFrom` tracks whether non-zero body bytes followed the boundary; the validate subcommand returns `ErrInlineBodyWithAtReference` when both are true.

**Exit-code mapping in the CLI:**

| Source | Mapped to |
|--------|-----------|
| `os.Open` failure for file path | 66 (EX_NOINPUT) |
| Bad CLI args, missing positional | 64 (EX_USAGE) |
| Any RFC 0001 violation (boundary, line, cross-line) | 65 (EX_DATAERR) |
| `io.Copy`/read error mid-stream after parse started | 74 (EX_IOERR) |

No panics, no logging. All errors propagate via return values. `cli_main.Run` (already used by existing utilities) handles top-level error printing and exit code mapping.

## Testing

**Three layers, scaling cheapest to most realistic:**

1. **Go unit tests** (`go/internal/charlie/hyphence/`):
   - `document_test.go` — `MetadataBuilder` round-trips, `Canonicalize` ordering and stability, comment anchoring (leading + trailing).
   - `consumers_test.go` — `MetadataStreamer` byte-exact passthrough, `MetadataValidator` against table-driven malformed lines, `FormatBodyEmitter` integration with `MetadataBuilder`.
   - `rfc_validate_test.go` — extends existing `testdata/rfc_vectors.txt` harness with one vector per new sentinel error. Adding a vector in TSV is enough to lock in coverage.

2. **Go subcommand tests** (`go/internal/india/commands_hyphence/`):
   - One small test per subcommand using `*bytes.Buffer` for stdin/stdout/stderr and a fixed source string. Verifies exit-code-to-error mapping, stdout content, stderr diagnostic format. No filesystem.

3. **Bats integration** (`zz-tests_bats/hyphence.bats`):
   - Build the real `hyphence` binary; round-trip through real files: capture-receipt fixture, inventory-log fixture, malformed inputs.
   - Check exit codes, stdout/stderr separation, `-` (stdin) handling, `format` idempotence (`format | format == format`).
   - Reuse fixture-staging conventions from existing `madder.bats` / `cutting-garden.bats`.

**Race coverage.** `just test-go-race` runs every package under `-race`. The new package and subcommands inherit automatically.

**Out of scope for v1** (filed as follow-ups):
- Cross-utility integration tests ([#130](https://github.com/amarbel-llc/madder/issues/130)).
- Performance benchmarks on large bodies.
- Fuzz testing on the validator.

## Build & flake integration

- Add `cmd/hyphence` to `go/default.nix` outputs alongside `madder`, `madder-cache`, `cutting-garden`, `madder-test-sftp-server`.
- Add `hyphence` to `go/cmd/madder-gen_man` so its man pages regenerate from `futility` metadata (matching the pattern for the other utilities).
- No flake input changes; no new dependencies.

## Rollback strategy

This is a **net-new utility** — no existing consumer to migrate, no breaking change. Rollback procedure if issues arise post-release: revert the commit series. No dual-architecture period needed because there's no prior architecture to preserve.

The new code in `go/internal/charlie/hyphence/` (`Document`, `Canonicalize`, the four consumer types, sentinel errors) is additive to the existing package. Removing it leaves the existing `Reader`/`Coder` machinery untouched. The new `commands_hyphence/` and `cmd/hyphence/` directories can be deleted wholesale.

If a specific subcommand turns out to be flawed but the rest is good, individual subcommands can be removed without touching the others — they're independent files in `commands_hyphence/`.

## Generality assessment

**Robustly generic** — these surfaces have no madder- or dodder-specific types and operate purely on the RFC 0001 wire format:

- `Document`, `MetadataLine`, `Canonicalize`, `Validate`
- `MetadataStreamer`, `MetadataBuilder`, `MetadataValidator`, `FormatBodyEmitter`
- The streaming-handler model (single-pass via `io.ReaderFrom` consumers stacked above the existing `hyphence.Reader`) is exactly what dodder's `text_parser2` already does at a higher level. A future dodder convergence (tracked in [#126](https://github.com/amarbel-llc/madder/issues/126)) would re-skin dodder's parser to consume this API and drop the duplicated boundary scanner.

**Bends if other consumers materialize:**

- `MetadataLine{Prefix, Value, LeadingComments}` doesn't model `! type@markl-id` or `- value < markl-id` locks as structured fields — they stay in `Value` as raw text. Tracked in [#128](https://github.com/amarbel-llc/madder/issues/128).
- `Canonicalize` uses stable-sort tie-breaking within prefix buckets, which the RFC doesn't normatively spec. Tracked in [#129](https://github.com/amarbel-llc/madder/issues/129).
- `MetadataValidator` is single-error-stop; `--all` mode for batch validation tracked in [#127](https://github.com/amarbel-llc/madder/issues/127).

## Plan handoff

Implementation plan to be authored next via `eng:writing-plans`. The plan should partition this design into:

1. The shared parser surface (`document.go`, `canonicalize.go`, `consumers.go`, tests, RFC vectors).
2. Each subcommand (`validate`, `meta`, `body`, `format`) — landable independently once the shared surface is in.
3. The utility scaffolding (`commands_hyphence/main.go`, `cmd/hyphence/main.go`, flake hookup, man pages).
4. Bats integration.

---

Filed by Clown on behalf of @friedenberg — https://github.com/amarbel-llc/clown :clown:
