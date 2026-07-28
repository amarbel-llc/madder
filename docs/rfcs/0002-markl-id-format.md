---
status: superseded
superseded-by: "piggy RFC 0011 (code.linenisgreat.com/linenisgreat/piggy docs/rfcs/0011-markl-id-format.md)"
superseded-date: 2026-07-20
date: 2026-05-10
authors: Sasha F (with Clown), drafted from amarbel-llc/madder#150
revisions:
  - 2026-05-09: initial draft (amarbel-llc/madder#150)
  - 2026-05-10: revert combined-HRP checksum rule to split-HRP form (amarbel-llc/madder#159)
  - 2026-06-09: add ssh_ecdsa_nistp256_pub format (§5) and piggy-piv_*/piggy-recipient-v1 purposes (§6.1), promoted to the normative cross-language subset
  - 2026-07-18: expand the purpose grammar from the `system-domain-role-version` registry convention to general identifiers (§2.1, §6); add the embedding-grammar quoting-split section (§2.2) (linenisgreat/madder#270)
  - 2026-07-20: superseded by piggy RFC 0011 (linenisgreat/madder#274)
---

# RFC 0002 — Markl ID Format

## Status

> **Superseded by [piggy RFC 0011](https://code.linenisgreat.com/linenisgreat/piggy/src/branch/master/docs/rfcs/0011-markl-id-format.md)**
> as of 2026-07-20, when the markl-id spec ownership moved to
> [`amarbel-llc/piggy`](https://code.linenisgreat.com/linenisgreat/piggy)
> under the piggy#183 inversion. Piggy RFC 0011 is the normative source.
>
> **Warning:** this document predates the 2026-07-20 rulings and does
> not reflect them:
>
> - narrowed purpose charset (bare-ident + quoted-String; `PurposeChar <- [a-zA-Z0-9_/-]`)
> - blech32 restricted to a single separator
> - lower-case only
> - checksum verification made normative for decoders
> - the legacy combined-HRP form marked historical (§9.1)
> - `@` legal inside a quoted purpose (piggy#227)
>
> Implement from piggy RFC 0011, not from this document.
>
> The revision history above is retained as lineage. See the git log for
> prior body content.
