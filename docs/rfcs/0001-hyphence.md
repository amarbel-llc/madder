---
status: proposed
date: 2026-05-02
authors: Sasha F (with Clown)
---

# RFC 0001 — Hyphence

## Status

Proposed. Will move to `accepted` upon merge of this RFC.

## Abstract

Hyphence is a text-based serialization format for a metadata section followed by an optional body. Used by madder for blob-store and tree-capture metadata, and by dodder for repository configs, blob store configs, workspace configs, type definitions, and zettels. This RFC is the prescriptive specification; the user-facing tutorial description lives in `docs/man.7/hyphence.md`.

## Notational Conventions

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in this document are to be interpreted as described in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) when, and only when, they appear in all capitals.

## Document Grammar

A hyphence document consists of:

- An optional **metadata section**, opened and closed by `BOUNDARY` lines.
- A blank **separator line** (REQUIRED if a body follows).
- An optional **body** of arbitrary bytes.

Production rules (informal):

```
DOCUMENT         = METADATA-SECTION? SEPARATOR? BODY?
METADATA-SECTION = BOUNDARY METADATA-LINE* BOUNDARY
BOUNDARY         = "---" LF
SEPARATOR        = LF                        ; one blank line
METADATA-LINE    = PREFIX SP CONTENT LF
PREFIX           = "!" / "@" / "#" / "-" / "<" / "%"
CONTENT          = arbitrary UTF-8 except LF
BODY             = arbitrary bytes
LF               = U+000A
SP               = U+0020
```

## Boundary Lines

A `BOUNDARY` line MUST consist of exactly three U+002D HYPHEN-MINUS characters followed by a single U+000A LINE FEED. Decoders MUST reject any other byte sequence in a boundary slot:

- Trailing space MUST cause rejection.
- Carriage return (U+000D) before the LINE FEED MUST cause rejection.
- Additional `-` characters (e.g. `----\n`) MUST cause rejection.

## Body Separator

When a body follows the metadata section, the closing boundary line MUST be followed by exactly one U+000A LINE FEED before the first byte of the body.

Decoders MUST reject input that contains a body without this blank line. Implementations MAY expose an opt-in lenient mode for reading legacy data — the Go reference implementation calls this mode `AllowMissingSeparator`. The lenient mode is NOT part of this specification; conforming emitters MUST always include the separator.

The strict requirement was added in response to dodder #41: lenient parsing silently produced objects with empty digests when the separator was omitted, masking a class of corruption.

## Metadata Lines

Each metadata line begins with a single-character `PREFIX`, a single space, and the content. The content extends to the next LINE FEED.

| Prefix | Name | Content |
|--------|------|---------|
| `!`    | type | Object type identifier. MAY include a lock as `! type@markl-id`. SHOULD be the last non-comment line. |
| `@`    | blob | Blob reference (markl-id or file-path). Alternative to inline body. |
| `#`    | description | Free text. Multiple description lines are concatenated with spaces. |
| `-`    | tag/reference | Opaque UTF-8 value with no LINE FEED. Convention: bare values are tags; values containing `/` are object references; either MAY carry a lock as `- value < markl-id`. |
| `<`    | object reference | Explicit object reference. Same syntax as `-` references. |
| `%`    | comment | Opaque, preserved during round-trips. Each comment is entangled with the non-comment line that follows it. |

Decoders MUST accept metadata lines in any order; line order MUST NOT affect semantics.

A document with both an `@` blob reference in the metadata AND a body section after the closing boundary is malformed. Decoders SHOULD reject such documents.

## Decoder Behavior

A conforming decoder:

- MUST reject input where the body lacks the separator (see "Body Separator"), unless an opt-in lenient mode is enabled.
- MUST accept metadata lines in any order.
- MUST treat unknown PREFIX characters as a parse error.
- MUST handle EOF mid-document as a parse error when either boundary is unmatched.
- SHOULD provide a typed error sentinel for the missing-separator case so callers can distinguish it from other errors. The Go reference implementation exposes `errMissingNewlineAfterBoundary`.

## Encoder Behavior

A conforming encoder:

- MUST emit boundary lines exactly as `"---\n"` (no trailing whitespace, no CRLF).
- MUST emit a single blank line between the closing boundary and the body when a body is present.
- MUST follow the canonical metadata-line order:
  1. Description lines (`#`)
  2. Locked object references
  3. Aliased object references
  4. Bare object references and tags (`-`)
  5. Blob line (`@`)
  6. Type line (`!`)
- MUST output a trailing LINE FEED after each metadata line.

## Versioning

Hyphence has no version indicator. The format itself is unversioned and stable; evolution is carried by type strings (e.g., `toml-blob_store_config-v2` succeeds `-v1`). Decoders MUST retain support for older type strings indefinitely.

This document is RFC 0001. Future format additions or revisions MUST be tracked in subsequent RFCs (e.g., `docs/rfcs/0002-hyphence-extensions.md`) that supersede or extend this one.

## Conformance

A conforming implementation of hyphence MUST satisfy the requirements in this RFC at the **wire-format level**. Implementations have latitude in their internal API design — typed wrappers, validation hooks, and ergonomic conveniences are not part of the spec. The wire format is.

### Required (MUST)

- All BOUNDARY-line, body-separator, and metadata-line requirements in sections **Document Grammar** through **Encoder Behavior**.
- Round-trip preservation: bytes written by a conforming encoder MUST decode to the same metadata fields and body bytes when read by any conforming decoder.
- All test vectors in `go/internal/charlie/hyphence/testdata/rfc_vectors.txt` MUST produce the documented outcome (see **Test Vectors**).

### Permitted (MAY)

- **Typed API wrappers.** An implementation MAY type-wrap any metadata-line content field as long as the wire serialization round-trips correctly. This includes validating structs, whitespace normalization, case folding, and ergonomic accessors. The Go reference implementation wraps the type identifier in `ids.TypeStruct` (which lower-cases and trims `! ` prefix characters); this is implementation latitude, not a wire-format requirement. A different implementation MAY use a plain `string` for the same field and remain conforming.
- **Lenient legacy-data mode.** An implementation MAY expose an opt-in mode that accepts input without the body separator (see **Body Separator**). Lenient mode is NOT part of the spec; documents written by conforming emitters MUST always include the separator. The Go reference implementation calls this mode `AllowMissingSeparator` and defaults it to `false`.
- **Performance and ergonomics.** Buffering strategies, streaming vs. batch decoding, error-context shaping, error-type granularity beyond the typed missing-separator sentinel, and similar implementation decisions are out of scope.

### Out of scope

- Implementation language, runtime, or platform.
- In-memory representation of decoded objects, persistence layers, caching.
- The internal format of the BODY itself — this RFC defines the envelope; the BODY's interpretation is the concern of the type identified by the `!` line.

### Wire-format extensions

Adding new PREFIX characters or modifying the semantics of existing ones is a **wire-format change**, not implementation latitude. Such changes MUST be tracked in a superseding or extending RFC (per **Versioning**). A decoder that encounters an unknown PREFIX MUST emit a parse error per **Decoder Behavior** — extensions are not silently forward-compatible. Two implementations that disagree on PREFIX semantics are NOT both conforming.

## Test Vectors

A normative set of test vectors lives at `go/internal/charlie/hyphence/testdata/rfc_vectors.txt`. Each non-comment, non-empty line is a tab-separated tuple:

```
NAME \t INPUT-B64 \t OUTCOME \t EXPECTED-B64
```

- `NAME` — short kebab-case identifier.
- `INPUT-B64` — base64-encoded raw input bytes.
- `OUTCOME` — one of:
  - `parse-ok` — decode MUST succeed; the captured body MUST equal `EXPECTED-B64` decoded.
  - `parse-error-missing-separator` — decode MUST fail with the typed missing-separator error.
- `EXPECTED-B64` — base64 expected body for `parse-ok`; `-` for errors.

A conforming implementation MUST pass every vector. The Go reference test harness is `TestRFCConformance_HyphenceTestVectors` in `go/internal/charlie/hyphence/rfc_conformance_test.go`.

New vectors MAY be added to the file at any time. Removing a vector requires a superseding RFC, since vectors are normative.

## See Also

- `docs/man.7/hyphence.md` — tutorial / reference manual.
- `go/internal/charlie/hyphence/` — Go reference implementation.
- `go/internal/charlie/hyphence/testdata/rfc_vectors.txt` — normative test vectors.
- [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) — normative-language conventions.
- [dodder #41](https://github.com/amarbel-llc/dodder/issues/41) — the production issue that motivated the strict body-separator rule.
- [madder #115](https://github.com/amarbel-llc/madder/issues/115) — the convergence work that made this RFC possible.
