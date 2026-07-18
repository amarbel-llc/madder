---
status: proposed
date: 2026-05-10
authors: Sasha F (with Clown), drafted from amarbel-llc/madder#150
revisions:
  - 2026-05-09: initial draft (amarbel-llc/madder#150)
  - 2026-05-10: revert combined-HRP checksum rule to split-HRP form (amarbel-llc/madder#159)
  - 2026-06-09: add ssh_ecdsa_nistp256_pub format (§5) and piggy-piv_*/piggy-recipient-v1 purposes (§6.1), promoted to the normative cross-language subset
  - 2026-07-18: expand the purpose grammar from the `system-domain-role-version` registry convention to general identifiers (§2.1, §6); add the embedding-grammar quoting-split section (§2.2) (linenisgreat/madder#270)
---

# RFC 0002 — Markl ID Format

## Status

Proposed. Will move to `accepted` upon merge of this RFC.

This RFC pins the wire format the Go reference implementation already
produces and consumes. No on-disk bytes change. The reference
implementation lives in
[`amarbel-llc/piggy`](https://github.com/amarbel-llc/piggy)'s
`go/` module (`github.com/amarbel-llc/piggy/go`; moved from madder's
`go/internal/bravo/markl/` under the piggy#183 ownership inversion),
alongside piggy's Rust `piggy-markl` crate. A normative spec plus
portable test vectors are the precondition for cross-language
compatibility without silent drift. Both repositories root their Go
module at `go/`, so repository-relative paths below are qualified in
prose: "piggy's" paths live in the piggy repository; "madder's" and
unqualified paths live in this (madder) repository.

## Abstract

A markl ID is a self-describing, checksummed, human-readable identifier
for binary data in the dodder/madder ecosystem. It encodes cryptographic
digests, signatures, and keys using *blech32*, a modified bech32
encoding. This RFC specifies the wire format normatively, registers the
canonical format-ID and purpose-ID values, and pins test vectors so
independent implementations can verify byte-exact compatibility.

## Notational Conventions

The key words **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and
**MAY** in this document are to be interpreted as described in
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) when, and only when,
they appear in all capitals.

References of the form *(test: TestX in path/to/file.go)* point to the
Go reference implementation's executing test that pins the claim. Every
normative requirement in this RFC has such a reference.

## 1. Motivation

Markl IDs are used as content-addressable blob digests in object
metadata, as signatures in inventory lists, as type locks in hyphence
documents, and as repository public keys. The Go reference
implementation (piggy's `go/internal/bravo/markl/`) is the
de-facto behaviour; this RFC formalises it.

## 2. Structure

A markl ID has the text form:

    [purpose@]format-data

The three parts are:

- **purpose** (OPTIONAL) — a semantic-context label. When present it
  MUST be followed by a literal `@` separator. Its grammar admits two
  overlapping classes — **registered** purposes, validated against the
  format-compatibility registry, and **general** purposes, opaque
  identifiers resolved by the consumer's own type system — given in
  §6. §2.2 covers how a markl ID, and its purpose slot specifically,
  embeds into larger textual grammars.
- **format** — the format identifier (e.g. `sha256`,
  `pivy_ecdh_p256_pub`). It MUST be one of the registered format IDs in
  §5, or a registered purpose-id alias resolving to one (§6.4).
- **data** — the blech32-encoded binary payload, including its
  6-character blech32 checksum (§3).

The blech32 separator (literal `-`) sits between `format` and `data`.
The checksum is computed over `format` only. The purpose, when
present, is textual decoration prepended to the blech32 string after
encoding — it is **not** part of the checksum input. Encoding the same
`(format, data)` under two different purposes therefore produces
identical blech32 bodies, differing only in their `<purpose>@`
prefixes.

A markl ID with empty `data` and unset `format` is the *null* state;
its canonical text form is the empty string. Implementations MUST NOT
produce a markl ID whose `data` portion is non-empty without an
accompanying format. *(test:
`TestInvariant_ZeroValueIdIsNullState`,
`TestIdNullAndEqual` in piggy's `go/internal/bravo/markl/`.)*

### 2.1. ABNF Grammar

```abnf
markl-id     = [ purpose "@" ] format "-" data
purpose      = 1*( purpose-char )                 ; general identifier; see §6
purpose-char = %x21-3F / %x41-7E                  ; VCHAR (%x21-7E) less "@" (%x40)
format       = 1*( ALPHA / DIGIT / "_" )          ; HRP component; see §5
data         = 7*( charset-char )                  ; >= 7 chars: 1+ payload + 6 checksum
charset-char = "q" / "p" / "z" / "r" / "y" / "9" / "x" / "8" / "g" / "f" /
               "2" / "t" / "v" / "d" / "w" / "0" / "s" / "3" / "j" / "n" /
               "5" / "4" / "k" / "h" / "c" / "e" / "6" / "m" / "u" / "a" /
               "7" / "l"
              ; charset string "qpzry9x8gf2tvdw0s3jn54khce6mua7l" — see §3
```

Uppercase forms of every byte above are also legal, subject to §3.5's
uniform-case rule.

The `purpose` production was widened on 2026-07-18
(linenisgreat/madder#270, ruled at
linenisgreat/hyphence#6) from the earlier
`system-domain-role-version` registry convention to this
general-identifier superset. The only markl-level constraints on a
purpose are structural: it MUST NOT contain `@` (reserved as the
purpose/digest separator; §4 step 1), and — because a space is not a
`VCHAR` byte — it cannot contain whitespace and still round-trip
through markl's own bare, unquoted text form (§2.2). Beyond those two
constraints the charset is open: `/` is explicitly legal (an
object-id-shaped purpose such as `one/uno`), as are runes that some
*particular* embedding grammar reserves for its own syntax (for
example trellis's `Reserved` set and sigil runes, cutting-garden
`docs/rfcs/0014-trellis.peg`) — such a purpose is still a legal markl
ID; it is that embedding grammar's job, not markl's, to quote it where
its own syntax requires (§2.2). Registered purposes (§6.1) additionally
MUST conform to the narrower `system-domain-role-version` naming
convention as a registration-time policy (§6.2); that convention is
not a wire-level constraint on purposes in general (§6).

### 2.2. Embedding and the Quoting Split

A markl ID's own text form (§2, §2.1) is bare: there is no escaping or
quoting mechanism defined at the markl layer itself, and it MUST NOT
contain whitespace. This holds regardless of how permissive the
purpose grammar becomes (§2.1) — a purpose value containing a space,
or any byte outside markl's own `purpose-char` charset, simply has no
direct bare-text serialization; it can be reached only through an
embedding grammar's quoting, as described below.

Larger textual grammars that embed a markl ID as a lexeme — trellis
(cutting-garden `docs/rfcs/0014-trellis.peg`, whose `Ident`/`IdentRune`
productions admit interior `@` so `purpose@format-data` parses as one
opaque identifier) and hyphence (its RFC 0002 content grammar, and the
forthcoming RFC 0003 lock-supersession) among them — MAY need to
represent a purpose containing runes their own grammar reserves (a
space, or a rune in that grammar's own `Reserved` set). When they do,
the embedding grammar MUST quote **the purpose slot only**:

    "my thing"@blake2b256-...

never the markl ID as a whole. The digest slot (`format-data`) MUST
remain outside any quoting — unquoted and structurally intact — so
tooling that operates on the digest independently of the purpose
(prefix elision, trie-abbreviation, diffing, the mother→child
digest-extraction paths of §9) can locate it without first parsing or
undoing the embedding grammar's quoting.

The quoting mechanism itself — which runes trigger it, what escape
sequences it supports — is defined by each embedding grammar, not by
this RFC; markl only requires that whatever mechanism is chosen quotes
the purpose slot in isolation, leaving the digest slot bare. A purpose
MUST NOT contain the literal `@` character under any circumstance,
quoted or not: `@` is markl's own purpose/digest separator (§4 step
1), and admitting it inside a quoted purpose would reintroduce the
ambiguity the split-HRP checksum rule (§3.3) and the first-`@` decode
rule (§4) exist to avoid. An embedding grammar that needs a literal
`@` as a purpose's *semantic* content — not as markl's join — has no
representation in a markl ID's purpose slot and MUST encode that value
some other way in its own grammar.

*(Ruled 2026-07-18: linenisgreat/hyphence#6,
linenisgreat/piggy#219, cutting-garden
`docs/rfcs/0014-trellis.peg` interior-`@` amendment.)*

## 3. Blech32 Encoding

Blech32 is identical to BIP173 bech32 except the separator between HRP
and data is the ASCII hyphen `-` (0x2D) instead of `1`. Like BIP173
bech32 (and unlike bech32m), the polymod XOR target is the constant
`1`. *(test: `TestBlech32` in piggy's
`go/internal/alfa/blech32/main_test.go`, plus
`TestRFC0002VectorsRoundTrip` in piggy's
`go/internal/charlie/markl_registrations/`.)*

### 3.1. Charset

The 32-character charset is:

    qpzry9x8gf2tvdw0s3jn54khce6mua7l

The alphabet excludes the visually ambiguous characters `1` (one),
`b` (bee), `i` (eye), and `o` (oh).

### 3.2. Separator

The separator is the ASCII hyphen `-`. Implementations MUST locate the
separator as the *last* `-` in the string (a markl ID's `purpose` MAY
contain hyphens; see §6).

### 3.3. Checksum

The checksum is a 6-character BCH code over the HRP-expansion
concatenated with the 5-bit data values. The generator polynomial is:

    [0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3]

The polymod XOR target is `1`. The HRP expansion is identical to
BIP173: each HRP byte contributes its top-3 bits, then a single zero
byte, then each HRP byte's low-5 bits.

For purpose-bearing markl IDs the HRP MUST be just `<format>` — the
purpose is **not** part of the HRP and does **not** contribute to the
checksum input. The purpose's role is identity decoration around the
digest, not part of the cryptographic content; binding the checksum to
the purpose would break legitimate digest-extraction paths (e.g.
mother→child signature lineage, audit references) that copy the same
digest bytes between purposes. *(test:
`TestRFC0002CrossPurposeBlech32Equal` in
`go/internal/charlie/markl_registrations/`.)*

### 3.4. Bit Conversion

Encoding converts the binary payload from 8-bit groups to 5-bit groups,
left-padding the final group with zero bits. Decoding converts 5-bit
groups back to 8-bit groups; padding bytes MUST be zero, and any
non-zero padding or unconsumed bits MUST cause the decode to fail.

### 3.5. Case

The entire markl ID string MUST be uniformly cased — all upper-case or
all lower-case. Mixed-case strings MUST be rejected. The all-upper
form is equivalent to its lowercased counterpart.

The canonical form pinned by this RFC's vectors (§7) is **lower-case**.

*(test: `TestRFC0002InvalidVectorsRejected/mixed_case` in
`go/internal/charlie/markl_registrations/`.)*

### 3.6. Length

The 90-character total-length limit from BIP173 §5 is **not**
enforced. Implementations MUST accept arbitrary-length markl IDs
subject to the data-portion-minimum of 7 characters (1+ payload byte +
6 checksum bytes). *(test: long-vector cases in `TestBlech32`.)*

## 4. Decoding Algorithm

Given an input string `S`:

1. Locate the *first* `@` in `S`. If present, split into `purpose`
   (before `@`) and `body` (after). Otherwise, set `purpose = ""` and
   `body = S`.
2. Validate `body` is uniformly cased per §3.5. If not, fail with
   `MixedCase`.
3. Locate the *last* `-` in `body`. If absent, fail with
   `SeparatorMissing`.
4. Split `body` into `hrp` (before the last `-`) and `data` (after).
   The `hrp` is `formatId`; it MUST NOT contain `@`.
5. Validate `len(data) >= 7`. If not, fail with `DataPortionTooShort`.
6. Validate every byte in `data` is in the charset of §3.1. If not,
   fail with `InvalidCharacter`.
7. Verify the blech32 checksum over (HRP-expand(hrp) || data-as-5-bit).
   If the polymod ≠ 1, fail with `InvalidChecksum`. The HRP here is
   `formatId` only; the purpose is **not** part of the checksum input.
8. Convert the first `len(data)-6` 5-bit values to 8-bit bytes per
   §3.4. Reject non-zero padding.
9. Resolve `formatId` against the format registry (§5), applying the
   purpose-id alias table (§6.4) if present. If unresolvable, fail
   with `UnknownFormat`.
10. If `purpose != ""` and `purpose` is present in the decoder's
    purpose registry (§6), validate `formatId` is among that purpose's
    compatible formats. If not, fail with
    `IncompatiblePurposeAndFormat`. If `purpose` is absent from the
    registry, accept the ID and carry the purpose opaquely (§6.6).
11. Validate `len(payload)` matches the resolved format's declared size
    (§5). If not, fail with `WrongSize`.

The order of step 1 (`@`-split) before step 7 (checksum) is
deliberate: the checksum MUST be verified over the `formatId` substring
only, not over a combined `<purpose>@<format>` string. Binding the
checksum to the purpose would change a digest's encoded form when its
identity decoration changes, breaking digest-extraction and
mother→child signature paths. *(test:
`TestRFC0002InvalidVectorsRejected` covers each terminal failure
category; `TestRFC0002CrossPurposeBlech32Equal` covers the
purpose-independent checksum rule.)*

## 5. Format ID Registry

Each format ID has a fixed payload size in bytes. Implementations MUST
reject markl IDs whose decoded payload does not equal the registered
size for the named format. *(test:
`TestInvariant_SetMarklId_WrongSize_Errors`,
`TestInvariant_SetHexBytes_WrongSize_Errors` in piggy's
`go/internal/bravo/markl/`.)*

| Format ID            | Size (bytes) | Description                                  |
|----------------------|--------------|----------------------------------------------|
| `sha256`             | 32           | SHA-256 digest                               |
| `blake2b256`         | 32           | BLAKE2b-256 digest                           |
| `ed25519_pub`        | 32           | Ed25519 public key                           |
| `ed25519_sec`        | 64           | Ed25519 private key (RFC 8032 §5.1.5 form)   |
| `ed25519_sig`        | 64           | Ed25519 signature                            |
| `ed25519_ssh`        | 32           | Ed25519 public key surfaced via SSH agent    |
| `ecdsa_p256_pub`     | 33           | ECDSA P-256 compressed public key (SEC 1)    |
| `ecdsa_p256_sig`     | 64           | ECDSA P-256 signature (r ‖ s, fixed-width)   |
| `ecdsa_p256_ssh`     | 33           | ECDSA P-256 public key via SSH agent         |
| `age_x25519_pub`     | 32           | age X25519 public key                        |
| `age_x25519_sec`     | 32           | age X25519 secret key                        |
| `pivy_ecdh_p256_pub` | 33           | PIV ECDH P-256 compressed public key (SEC 1) |
| `ssh_ecdsa_nistp256_pub` | 33       | SSH-suitable ECDSA P-256 public key, SEC1-compressed |
| `nonce`              | 32           | Random nonce                                 |

The `*_ssh` formats carry a bare public-key payload (32 or 33 bytes);
the SSH-agent integration that produces signatures with these keys is
implementation-internal and not part of the wire format. Earlier
informal documentation described these formats as "variable size" —
that was incorrect.

`ssh_ecdsa_nistp256_pub` is byte-identical in shape to `ecdsa_p256_pub`
(both are 33-byte SEC1-compressed P-256 points). The distinct format ID
exists so a purpose (§6.1) can distinguish a PIV slot's SSH-suitable
authentication/signature key (`piggy-piv_*-v1`) from a repository or
recipient public key of the same shape, preventing the format-confusion
attack described in §8 item 3. This format is owned jointly with
[`amarbel-llc/piggy`](https://github.com/amarbel-llc/piggy), which
mirrors it in its `piggy-markl` Rust crate.

### 5.1. Registering New Formats

A new format ID MUST be added to this RFC by amendment. The format ID
MUST conform to the lexical rule in §2.1 (`format`) and MUST NOT
collide with any prefix that would change a previously valid markl
ID's interpretation.

## 6. Purpose ID Registry

A purpose is either **registered** — validated against a
(purpose, compatible-format) entry in this section, and named per the
`system-domain-role-version` convention (§6.2) — or **general**: an
opaque identifier from a consumer's own type system (a hyphence type
name, a zettel id, a typed-edge field name), unconstrained beyond
§2.1's wire-form charset and carried opaquely per §6.6. Both classes
share the same `purpose@format-data` text form and nothing in the wire
syntax distinguishes them; classification is entirely a registry
lookup at decode time (present → registered semantics apply; absent →
general/opaque). The purpose appears textually *before* the `@`
separator in a markl ID; it is **not** part of the blech32 HRP (§3.3)
and does not contribute to the checksum.

Purpose-full markl IDs are the canonical spelling for pinned/locked
references across the ecosystem: a type pinned to its definition
(`md@blake2b256-...`), an object pinned to a version
(`one/uno@blake2b256-...`), a typed edge pinned to a target
(`blocks=task/other@blake2b256-...`) — alongside the existing
registered-purpose uses (`piggy-piv_auth-v1@ssh_...`). This dual role
is what motivated the 2026-07-18 general-identifier expansion
(linenisgreat/hyphence#6, linenisgreat/piggy#219,
linenisgreat/madder#270): the purpose slot needed to carry not just
registry-scheme names but arbitrary consumer-side identifiers.

### 6.1. Registered Purposes

Purpose IDs are **owned by the system named by their prefix**: madder
owns the registration *mechanism* (§6.3) and the `madder-*` namespace;
every other purpose is owned by its consumer system (`dodder-*` by
dodder, `piggy-*` by piggy, `papi-*` by papi). The table below is the
consumer-owned registry snapshot mirrored by the Go reference
implementation; each row's semantics are authoritative in the owning
system's documentation.

This subsection pins the **stable cross-language subset** of purpose
IDs. Independent implementations MUST support these. IDs bearing any
other purpose MUST NOT be rejected merely for being unknown — they
decode opaquely per §6.6.

| Purpose                          | Owner  | Compatible Formats                              | Description              |
|----------------------------------|--------|-------------------------------------------------|--------------------------|
| `dodder-blob-digest-sha256-v1`   | dodder | `sha256`, `blake2b256`                          | Blob content hash        |
| `dodder-object-digest-v2`        | dodder | `sha256`, `blake2b256`                          | Object metadata hash     |
| `dodder-object-digest-v3`        | dodder | `sha256`, `blake2b256`                          | Object metadata hash (covers typed blob references) |
| `dodder-object-sig-v2`           | dodder | `ed25519_sig`, `ecdsa_p256_sig`                 | Object signature         |
| `dodder-object-sig-v3`           | dodder | `ed25519_sig`, `ecdsa_p256_sig`                 | Object signature (over the v3 digest) |
| `dodder-object-mother-sig-v3`    | dodder | `ed25519_sig`                                   | Object mother signature (v3 lineage) |
| `dodder-repo-public_key-v1`      | dodder | `ed25519_pub`, `ecdsa_p256_pub`                 | Repository public key    |
| `dodder-repo-private_key-v1`     | dodder | `ed25519_sec`, `ed25519_ssh`, `ecdsa_p256_ssh`  | Repository private key   |
| `piggy-piv_auth-v1`              | piggy  | `ssh_ecdsa_nistp256_pub`                        | PIV slot 9A public key (Authentication) |
| `piggy-piv_sig-v1`               | piggy  | `ssh_ecdsa_nistp256_pub`                        | PIV slot 9C public key (Digital Signature) |
| `piggy-piv_card_auth-v1`         | piggy  | `ssh_ecdsa_nistp256_pub`                        | PIV slot 9E public key (Card Authentication) |
| `piggy-recipient-v1`             | piggy  | `pivy_ecdh_p256_pub`, `age_x25519_pub`          | Encryption recipient (PIV slot 9D ECDH key, or age recipient) |
| `papi-doc-sig-v1`                | papi   | `ecdsa_p256_sig`                                | PAPI document signature (slot-9A SSH sig over JCS bytes) |

The `piggy-*` purposes are owned jointly with
[`amarbel-llc/piggy`](https://github.com/amarbel-llc/piggy) and mirrored
in its `piggy-markl` Rust crate
(`crates/piggy-markl/src/{format,purpose}.rs`). They are surfaced by
`piggy list` and consumed by madder wherever a piggy-issued key appears
in a markl-id slot.

The `papi-doc-sig-v1` purpose is owned jointly with
[`amarbel-llc/papi`](https://github.com/amarbel-llc/papi) and mirrored in
the `piggy-markl` Rust crate for the producer side (`piggy papi sign`).
Its payload is the 64-byte `ecdsa_p256_sig` (r ‖ s, fixed-width) produced
by a YubiKey PIV slot-9A `ecdsa-sha2-nistp256` key signing a PAPI
document's canonicalized (JCS) bytes, with the SSH-wire signature framing
stripped. It spans only `ecdsa_p256_sig`: PAPI's slot-9A co-sign model is
P-256 throughout, and widening a purpose's compatible-format set is a
backward-compatible amendment (existing IDs still validate), so the
registration starts narrow.

*(test: `TestRFC0002VectorsRoundTrip/purpose/...` in piggy's
`go/internal/charlie/markl_registrations/`, plus
`TestAllPurposes_Registered`,
`TestAllPurposes_RelatedRoundTrip` in madder's
`go/internal/charlie/markl_registrations/`.)*

The owning systems register additional purposes outside this table
(dodder: `dodder-object-{digest-sha256,sig,mother-sig}-v1`,
`dodder-object-metadata-digest-without_tai-v1`, `dodder-repo-sig-v1`,
`dodder-request_auth-{challenge,response,repo-sig}-v1`; madder:
`madder-public_key-v1`, `madder-private_key-{v0,v1}`,
`madder-blob_store-config-digest-v1`). These are **out of scope** for
this RFC: they remain valid wire-format markl IDs, but their
semantics are not pinned cross-language. Future RFCs MAY promote any
of them into §6.1.

### 6.2. Registering New Purposes

The rules in this subsection govern purposes seeking *registration*
(format-compatibility validation, §6.5 Related-role support). A
general/unregistered purpose (§6, §6.6) need not conform to rule 1's
naming convention — its charset is governed only by §2.1.

A new purpose ID MUST be added by amendment. The purpose ID MUST:

1. Conform to `system-domain-role-version`, with `version` as `v`
   followed by one or more digits.
2. Enumerate its compatible format IDs.
3. Document the semantic role of the data so independent
   implementations can verify they're using the right key in the right
   context.

Implementations MUST reject markl IDs whose purpose is registered but
whose `formatId` is not among that purpose's compatible formats
(`IncompatiblePurposeAndFormat`). IDs bearing a purpose absent from
the registry MUST be accepted and carried opaquely (§6.6).

### 6.3. Per-Binary Registration

The framework code (piggy's `go/internal/bravo/markl/`) does not
contain the purpose registrations; each consumer installs its own on
init. Piggy's module registers the formats and the `piggy-*` purposes
(`go/internal/charlie/markl_registrations/`); madder registers
the `madder-*` and (transitionally) `papi-doc-sig-v1` purposes plus
the legacy purpose-id aliases (madder's
`go/internal/charlie/markl_registrations/`); dodder registers the
`dodder-*` purposes in its own tree. Any
consumer MAY register additional purposes via
`markl.RegisterPurpose` without forking the framework. See
[ADR 0006](../decisions/0006-markl-registration-api-shape.md). This
property is normative for the registration API, not the wire format —
the wire format only sees a flat map of purposeId → compatible
formatIds at decode time.

### 6.4. Purpose-ID Aliases

Pre-RFC dodder data wrote markl IDs whose HRP was a *purpose-id-shaped*
string (no `@` separator) — i.e. the purpose ID sat in the format-id
slot. The current parser resolves such an HRP through an **alias
table** that maps purposeId-shaped strings to canonical format IDs.

Implementations supporting legacy-data interop MUST honour this alias
table. Implementations targeting only forward-compatible data MAY omit
it, in which case those IDs decode as `UnknownFormat`. The currently
registered aliases are:

| Alias purposeId               | Resolved formatId   |
|-------------------------------|---------------------|
| `dodder-repo-private_key-v1`  | `ed25519_sec`       |
| `zit-repo-private_key-v1`     | `ed25519_sec`       |

*(test: `TestAllAliases_ResolveViaGetFormatOrError` in madder's
`go/internal/charlie/markl_registrations/`;
`TestRFC0002AliasResolution` in piggy's
`go/internal/charlie/markl_registrations/`.)*

Note that the alias table and the §6.1 purpose registry are separate
namespaces that happen to share the `dodder-repo-private_key-v1`
identifier. New data SHOULD use the modern form
(`<purpose>@<format>-<data>`) where the format-id slot carries an
actual format ID.

### 6.5. Related Roles

Purposes MAY carry a free-form `Related` map of role-name →
purposeId-string pairs, used by signing and key-derivation paths to
walk between paired purposes (e.g. a sig purpose's `digest` role
points at the corresponding digest purpose). The role names used by
madder's own purposes are `digest`, `mother_sig`, and `public_key`.
Other consumers MAY define additional role names; markl itself stays
role-agnostic per ADR 0006.

The `Related` map is part of the registration API, not the wire
format. *(test: `TestAllPurposes_RelatedRoundTrip`,
`TestPurposeRepoPrivateKeyV1_RelatedPublicKey` in madder's
`go/internal/charlie/markl_registrations/`.)*

### 6.6. Unknown Purposes

A decoder MUST accept a syntactically valid markl ID whose purpose is
absent from its registry, carrying the purpose as an opaque string:
round-tripping the ID MUST preserve the purpose byte-for-byte, and the
§4 structural validations (checksum, charset, payload size) still
apply in full. Purpose-format compatibility (§4 step 10) is only
enforceable for registered purposes.

This rule is what decouples consumers: an owning system may mint IDs
under a newly registered purpose (§6.2, §6.3) without requiring every
other implementation to upgrade in lockstep. Opacity licenses
transport and storage, not interpretation — contexts that need the
purpose's *semantics* (signature verification, key derivation,
Related-role walks per §6.5) MUST still fail on an unknown purpose.

Since 2026-07-18 (linenisgreat/madder#270), this rule also covers
**general identifiers used as purposes by design**, not only purposes
awaiting registration: a hyphence type name, a zettel id
(`one/uno`), or a typed-edge field name used as a purpose (§6 —
`md@...`, `one/uno@...`, `blocks=task/other@...`) is never expected
to appear in this registry; resolving it is the consuming type
system's job, exactly as resolving any other identifier is
(linenisgreat/hyphence#6). Decoders MUST NOT treat "purpose absent
from registry" as an error condition or a sign of stale data.

*(test: `TestSetMarklId_UnknownPurposeAcceptedOpaquely` in piggy's
`go/internal/bravo/markl/`; fixture vector
`purpose/example-unregistered-purpose-v1/sha256`.)*

## 7. Test Vectors

Independent implementations MUST round-trip the conformance fixture at
piggy's
`go/internal/charlie/markl_registrations/testdata/0002-markl-id-format-vectors.json`
(canonical home since the piggy#183 ownership inversion; madder's copy
was retired with the cutover). The fixture is the canonical artifact;
this section documents only its schema. The file lives under Go's `testdata/` convention so it travels
with the Go module's build sandbox; it is otherwise readable as plain
JSON by any consumer.

### 7.1. Vector File Schema

```json
{
  "vectors": [
    {
      "name": "format/blake2b256/non_trivial",
      "purpose": "",
      "format": "blake2b256",
      "payload_hex": "000102…",
      "encoded": "blake2b256-…"
    }
  ],
  "invalid": [
    {
      "name": "mixed_case",
      "encoded": "Sha256-…",
      "error": "MixedCase"
    }
  ]
}
```

A round-trip implementation:

1. Reads `payload_hex`, decodes to bytes.
2. Encodes via the implementation under test with `format` and (if
   non-empty) `purpose`.
3. Asserts the result equals `encoded` (canonical lowercase form).
4. Decodes `encoded` and asserts it produces `(purpose, format,
   bytes)`, applying the §4 validations.

For invalid vectors, the implementation MUST reject `encoded`. The
`error` field names a structural failure category from §4 — the exact
error type is implementation-specific.

### 7.2. Concrete Vectors

The Go reference implementation generates the fixture
deterministically via a build-tag-gated test
(`TestGenerateRFC0002Vectors`, gated by `rfc0002_generate`) and
verifies it on every CI run via `TestRFC0002VectorsRoundTrip` /
`TestRFC0002InvalidVectorsRejected`. Three of the six invalid vectors
(`mixed_case`, `wrong_size_for_format`,
`incompatible_purpose_format`) double as **poison vectors** that fail
when the corresponding decoder validation is removed; this RFC's
preparation involved demonstrating each one against a deliberately
de-validated decoder before re-applying the fixes.

The fixture covers, at minimum:

- One round-trip vector per registered format (§5) with payload bytes
  `[0x00, 0x01, …, size-1]`.
- An additional all-zeros vector for each hash format (the format's
  canonical null state).
- One round-trip vector per `(purpose, compatible-format)` pair from
  §6.1.
- One round-trip vector bearing a purpose absent from every registry,
  pinning the §6.6 opaque-carry rule.
- Invalid vectors covering: mixed case, missing separator, wrong
  checksum, charset violation, wrong payload size, incompatible
  `(purpose, format)` pair.

To regenerate after a registry change (in the piggy repository):

```sh
cd go && go test -tags 'test rfc0002_generate' \
  -run TestGenerateRFC0002Vectors \
  ./internal/charlie/markl_registrations/...
```

## 8. Security Considerations

1. **Checksum is detection-only.** The 6-character BCH checksum
   detects transcription errors; it provides **no** protection against
   deliberate tampering. Implementations MUST NOT treat checksum
   validity as evidence of authenticity. Authenticity is provided by
   the cryptographic content identified by the markl ID (digests,
   signatures, key bindings).

2. **Length unbounded.** Because §3.6 lifts BIP173's 90-character cap,
   decode implementations MUST tolerate long inputs but SHOULD enforce
   a per-application maximum to prevent resource-exhaustion. A
   practical maximum for non-`*_ssh` formats is 130 characters
   (sufficient for a 64-byte payload plus the longest registered
   format/purpose names).

3. **Format ID is not authenticated.** The format ID is part of the
   HRP and so part of the checksum input, making it tamper-evident,
   but it is not authenticated by any signature. Implementations MUST
   validate the decoded payload size against the format's registered
   size (§5) and MUST validate `(purpose, format)` compatibility
   (§6.1), to prevent format-confusion attacks where a 33-byte
   `pivy_ecdh_p256_pub` is reinterpreted as some other 33-byte
   format.

4. **Case-equivalence is benign.** Upper and lower forms of a markl ID
   encode the same bytes. Stores and dedup logic MUST canonicalise to
   lower-case before content-addressed comparison.

## 9. Backwards Compatibility

Existing dodder/madder data on disk uses lower-case markl IDs —
without purposes for blob digests, with purposes for object metadata,
signatures, and repository keys, and with bare purpose-id-shaped HRPs
for legacy private-key references (resolved via §6.4). This RFC does
not change any wire byte; it pins the behaviour already implemented by
the Go reference implementation (piggy's
`go/internal/bravo/markl/`). Existing data remains valid.

The conformance work in
[#150](https://github.com/amarbel-llc/madder/issues/150) tightened
two decoders to match this spec where they previously diverged:

- `markl.Id.UnmarshalText` now runs the §4 size and (purpose, format)
  compatibility checks (previously skipped).
- `blech32.Decode` now validates uniform-case across the whole input
  (previously checked only the data portion).

These tightenings reject inputs the prior implementation silently
accepted. No prior input that was actually valid per this RFC is
affected.

A third tightening — binding the blech32 checksum to
`<purpose>@<format>` rather than just `<format>` — was incorrect and
has been **reverted** under
[#159](https://github.com/amarbel-llc/madder/issues/159). The
combined-HRP rule shipped briefly between commits `8dc78c7` and the
issue-#159 revert. The current spec restores the property that the
same `(format, data)` under different purposes encodes to identical
blech32 bodies — load-bearing for dodder's mother→child signature
lineage and any digest-extraction path that re-attaches a digest under
a different purpose. Existing pre-`8dc78c7` on-disk data is
checksum-verifiable again under the restored rule; downstream
consumers (dodder, piggy) coordinating on this spec MUST use the
split-HRP form.

## 10. References

### 10.1. Normative

- BIP 173 — Base32 address format for native v0-16 witness outputs (https://github.com/bitcoin/bips/blob/master/bip-0173.mediawiki)
- RFC 2119 — Key words for use in RFCs to Indicate Requirement Levels
- RFC 4253 — The Secure Shell (SSH) Transport Layer Protocol
- RFC 8032 — Edwards-Curve Digital Signature Algorithm (EdDSA)
- SEC 1 — Elliptic Curve Cryptography (compressed point format)

### 10.2. Informative

- BIP 350 — Bech32m format for v1+ witness addresses (cited only to
  clarify that blech32 uses bech32's polymod-XOR target `1`, not
  bech32m's `0x2bc830a3`)
- piggy `go/internal/bravo/markl/` — Go reference implementation
- piggy `go/internal/alfa/blech32/` — Go reference blech32
  implementation
- piggy `go/internal/charlie/markl_registrations/` — format and
  `piggy-*` purpose registrations; madder's
  `go/internal/charlie/markl_registrations/` — `madder-*`,
  transitional `dodder-*`, and papi purpose/alias registrations
- piggy `go/internal/charlie/markl_registrations/testdata/0002-markl-id-format-vectors.json` —
  conformance fixture (this RFC §7)
- `docs/man.7/markl-id.md` — informal manual page; this RFC supersedes
  it for normative purposes
- `docs/decisions/0006-markl-registration-api-shape.md` — ADR for
  `RegisterPurpose` API shape
- amarbel-llc/piggy issue #68 — original motivation for pinning the
  spec
- [linenisgreat/hyphence#6](https://code.linenisgreat.com/linenisgreat/hyphence/issues/6) —
  ruling that markl-id form is canonical for pinned/locked references
  ecosystem-wide; motivates the §2.1 purpose charset expansion and the
  §2.2 embedding-grammar quoting split
- [linenisgreat/piggy#219](https://code.linenisgreat.com/linenisgreat/piggy/issues/219) —
  implementation sibling tracking this amendment: piggy-side purpose
  grammar/parser expansion
- cutting-garden `docs/rfcs/0014-trellis.peg` — `Ident`/`IdentRune`
  productions; the 2026-07-18 interior-`@` amendment that makes
  `purpose@format-data` parse as one opaque identifier in trellis
- hyphence RFC 0003 (in progress, branch `kind-fig`, not yet merged as
  of this amendment) — supersedes hyphence RFC 0002's spaced `Lock`
  form with the purpose-full markl-id spelling this amendment supports

## Appendix A. Differences from BIP173 bech32

| Property                | BIP173 bech32      | Blech32                  |
|-------------------------|--------------------|--------------------------|
| Separator               | `1`                | `-`                      |
| Polymod XOR target      | `1`                | `1` (same)               |
| Charset                 | bech32 alphabet    | bech32 alphabet (same)   |
| Generator polynomial    | bech32 generator   | bech32 generator (same)  |
| 90-char length limit    | enforced           | not enforced             |
| Case rules              | uniform case       | uniform case (same)      |

The single substantive difference is the separator. The change of
separator makes blech32 visually distinct from bitcoin addresses while
preserving the checksum's detection properties.
