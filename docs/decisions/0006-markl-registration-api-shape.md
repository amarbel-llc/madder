---
status: proposed
date: 2026-05-02
decision-makers: Sasha F
---

# Shape of the public `markl` registration API

## Context and Problem Statement

[#106](https://github.com/amarbel-llc/madder/issues/106) proposes promoting `markl`'s package-private `makePurpose` / `makeFormat` to a public registration API so that downstream consumers (notably dodder) can register their own purposes and formats without forking `internal/bravo/markl`. Step 1 of that issue is the API-design piece — adding the public surface in place, before any registrations move out.

Three sub-shapes of that API are not obvious from the issue alone and need to be pinned down (tentatively) before the patch lands: how strictly `RegisterPurposeOpts.Related` is validated, whether role names are typed, and whether `RegisterPurpose` returns its `Purpose`. This ADR captures the current leans so the implementation can proceed; it stays at `proposed` because steps 2–4 of #106 may surface reasons to revisit them.

## Decision Drivers

* **Tracer-bullet discipline.** Step 1 must preserve byte-identical observable behavior. Any choice that changes when validation fires, or that reshapes how existing callers register purposes, has to be justified against the do-nothing baseline.
* **Codec/registry stability convention.** Existing memory: reserved types are locked, panic-on-duplicate, no shadow/override. The new API should match — same behavior at the public boundary as at the private one it replaces.
* **Init-order independence.** Purposes can reference other purposes (`Related: {"digest": PurposeObjectDigestV1}`). If `RegisterPurpose` validates references eagerly, every consumer has to hand-order their `init()`s. That's cheap inside a single package but costly across madder + dodder + future consumers.
* **Caller ergonomics over implementer ergonomics.** Madder owns one consumer site (its own `init()`s); dodder will own another. A small papercut in the public API multiplies across consumers, so prefer ergonomics on the calling side even when it costs the registry.
* **Reversibility.** All three sub-decisions can be tightened later (lazy → eager validation, free-form → typed roles, ignored return → required return) without breaking existing callers. Loosening them is harder. Start permissive; tighten if real misuse appears.

## Considered Options

### Sub-decision A: validation timing for `RegisterPurposeOpts.Related`

1. **Lazy.** `RegisterPurpose` stores the `Related` map verbatim. Lookups via `purpose.GetRelated(role)` succeed even if the referenced purpose is never registered; the consumer notices when something downstream calls `GetPurpose(returnedId)` and that panics. Matches today's `GetDigestTypeForSigType` behavior — switch arms only fire on lookup.
2. **Eager.** `RegisterPurpose` panics if any value in `Related` names a purpose not yet registered. Catches typos at startup but forces every consumer to register sigs after digests, mother-sigs after digests, etc.
3. **Eager with deferred resolve.** Collect a pending list, resolve at first `GetPurpose` call or via an explicit `markl.SealRegistry()`. Adds a lifecycle phase.

### Sub-decision B: role typing for `Related`

1. **Free-form `map[string]string`.** Roles are arbitrary strings. Madder's `markl_registrations` defines whatever role constants it needs (`"digest"`, `"mother_sig"`); dodder defines its own.
2. **Typed `map[RelatedRole]string`** with `RelatedRole` exported and a small set of madder-blessed constants (`RelatedRoleDigest`, `RelatedRoleMotherSig`, ...).
3. **Per-role accessor methods on `Purpose`** (e.g. `GetDigestPurpose()`, `GetMotherSigPurpose()`). No generic `GetRelated` at all.

### Sub-decision C: return value of `RegisterPurpose`

1. **Return the constructed `Purpose`.** Mirrors a value-returning constructor; lets callers assign to a package-level `var` if they want a typed handle.
2. **Return nothing.** Matches today's `makePurpose` signature exactly.
3. **Return `(Purpose, error)`.** Surfaces duplicate-registration as an error rather than a panic.

## Decision Outcome

Chosen option for each sub-decision (all `proposed`, may be revisited during steps 2–3 of #106):

* **A — Lazy validation.** `RegisterPurpose` does not check that values in `Related` name registered purposes. Document the behavior; let lookup-time panics catch typos.
* **B — Free-form `map[string]string` roles.** Madder's `markl_registrations` package owns its own role-name constants; dodder owns its own. `markl` itself stays role-agnostic.
* **C — Return the `Purpose`.** `RegisterPurpose(opts) Purpose`; callers may ignore it. Same for `RegisterFormat` returning the registered format value.

Together: "Chosen option: a permissive, role-agnostic registration API that mirrors existing private behavior, accepting deferred error detection in exchange for init-order independence and minimal cross-consumer coupling."

### Consequences

* Good — Init-order between madder's purposes and dodder's purposes is decoupled. A downstream consumer can register `dodder-object-sig-v1` with `Related: {"digest": "dodder-object-digest-v1"}` regardless of which package's `init()` runs first.
* Good — `markl` itself stays free of dodder vocabulary. Role names like `"digest"` and `"mother_sig"` live with the consumer that defines them, not in the framework package.
* Good — Step 1 stays a true tracer bullet: panic-on-duplicate, no eager validation, no new lifecycle phases. Diff against existing behavior is additive.
* Bad — Typos in `Related` keys or values surface at lookup time, not registration time. A consumer who writes `Related: {"diest": ...}` only finds out when something calls `GetRelated("digest")` and gets `(_, false)`.
* Bad — Free-form roles mean two consumers can collide on a role name with different semantics (`"digest"` meaning different things in madder vs. some third consumer). No registry-level protection.
* Bad — Return value of `RegisterPurpose` is unused in madder today. It's a small footprint cost (a dead value at every call site) for an option we're not exercising yet.

### Confirmation

Step 1 of #106 lands when:

1. `RegisterPurpose`, `RegisterFormat`, `RegisterPurposeIdAlias`, and `Purpose.GetRelated` exist as public symbols in `internal/bravo/markl`.
2. The package's existing `init()` blocks (in `purposes.go` and `format.go`) compile and pass tests with calls rewritten to use the new public funcs.
3. `GetDigestTypeForSigType` and `GetMotherSigTypeForSigType` are reimplemented as thin `purpose.GetRelated(...)` lookups, with their `Related` data registered alongside each sig purpose. Existing callers see no behavioral change.
4. The hardcoded `case "zit-repo-private_key-v1", "dodder-repo-private_key-v1"` switch in `GetFormatOrError` is gone, replaced by a `RegisterPurposeIdAlias` call from an `init()` (still inside the package; relocates in step 2).
5. `just test-go` passes.

If steps 2–3 of #106 surface a case where lazy validation, free-form roles, or unused return values cause real friction, this ADR is revised (or superseded) and the API tightened.

## More Information

* Tracking issue: [#106](https://github.com/amarbel-llc/madder/issues/106)
* Sibling extensibility issue: [#105](https://github.com/amarbel-llc/madder/issues/105)
* Affected files at time of writing:
  * `go/internal/bravo/markl/purposes.go` — `makePurpose`, `Purpose`, `GetDigestTypeForSigType`, `GetMotherSigTypeForSigType`
  * `go/internal/bravo/markl/format.go` — `makeFormat`, `GetFormatOrError` (the `zit`/`dodder` alias switch lives at lines 127–129)
