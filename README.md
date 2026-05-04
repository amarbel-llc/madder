# madder

Content-addressable blob storage CLI.

## Philosophy

Madder is not a graph. From outside the store, every blob is atomic
and fully resolved: `MakeBlobReader(id)` returns the decoded bytes,
the consumer never names a sidecar, and the markl-id is the only
handle a caller ever needs.

Inside the store, plugins MAY use sidecar data (trained dicts,
encryption keys, compression state) to deliver that surface — and
they own the mechanics of fetching, sync-transfer, and lifecycle for
their own data. Self-containedness is an API contract, not a
byte-layout claim.

What madder still doesn't do:

- Expose a graph or relationship layer at the API surface. Tools
  like dodder and cutting-garden compose blobs into graphs above
  madder; the store itself doesn't know about those edges.
- Allow consumers to assemble blobs from foreign references. Every
  decode must be resolvable inside the store the blob lives in,
  using only that store's plugin layer. Cross-store reads require
  sync, not link-following.

When a feature would surface relationships between blobs to
external consumers — references, joins, queries — that feature
belongs in a layer above madder.
