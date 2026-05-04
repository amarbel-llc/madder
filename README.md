# madder

Content-addressable blob storage CLI.

## Philosophy

**Madder is not a graph.** Its blobs are opaque, self-contained, and
content-addressed. Each blob can be read on its own — the bytes on
disk plus a hash format are sufficient to verify and decode the
content. There is no inter-blob reference, no link to resolve, no
sidecar required.

This is a deliberate constraint, not a gap. Tools built on top of
madder — dodder, cutting-garden, and others — are free to compose
blobs into graphs, define wire formats with embedded references,
trained-dictionary chains, or schema registries. Madder itself
stays out of that layer.

Concretely:

- **No cross-blob references at the storage layer.** A blob never
  needs another blob to be readable. Compression is per-blob;
  encoding chains are per-store, not per-graph.
- **No metadata graph.** Madder doesn't track which blob "uses"
  another. If a higher-level utility wants that, it owns the index.
- **No coupled lifecycles.** Deleting one blob never silently breaks
  another.

When a feature would require madder to track relationships between
blobs to function, that feature belongs in a layer above madder.
