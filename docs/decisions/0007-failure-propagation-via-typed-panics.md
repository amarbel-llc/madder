---
status: relocated
date: 2026-05-27
decision-makers: Sasha F
---

# Use typed panics as the failure-propagation channel (relocated)

> **This ADR has moved.**
>
> The canonical home is now in dewey, where the underlying
> machinery (`errors.Context`, `Cancel`, `runRetry`,
> `ContextContinueOrPanic`) lives:
>
> → [amarbel-llc/purse-first — `libs/dewey/docs/decisions/0001-typed-panics-as-failure-channel.md`](https://code.linenisgreat.com/purse-first/blob/master/libs/dewey/docs/decisions/0001-typed-panics-as-failure-channel.md)
>
> The relocated version is broadened from this repo's `BlobStore`
> framing to dewey's general panic-as-channel pattern, with
> madder's `BlobStore` retained as the worked example. All
> technical content from the original is preserved.

## Why the move

The story spanned three repos:

- this repo's ADR 0007 (madder, `BlobStore` motivation),
- [amarbel-llc/dodder#27](https://github.com/amarbel-llc/dodder/issues/27)
  (the parallel ADR-to-be in dodder),
- [amarbel-llc/madder#20](https://github.com/amarbel-llc/madder/issues/20)
  (this repo's cleanup pass).

All three concern dewey's panic-as-channel mechanism. Keeping the
canonical ADR next to dewey itself avoids the question "which
repo owns the decision?"

## Local relevance

madder still implements the pattern. The "Worked example:
madder's BlobStore" section of the relocated ADR keeps the
specific case study with all original detail (the SFTP `9b74186`
panic refactor, the `b3e0ae9` recover-boundary commits, madder#134
as the motivating issue). When working on madder's `BlobStore`,
backends, or `madder-mcp` recover boundaries, read the relocated
ADR; the conventions documented there apply here.

## Original commit

This file's content prior to relocation is preserved in commit
`d499f22` ("docs(adr): capture failure-propagation-via-typed-panics
decision"). The git history is the authoritative record of the
original framing.
