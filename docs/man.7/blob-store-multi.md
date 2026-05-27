---
author:
-
date: May 2026
title: BLOB-STORE-MULTI(7) Madder \| Miscellaneous
---

# NAME

blob-store-multi - madder Multi blob store primitive

# DESCRIPTION

**Multi** is an in-process composition primitive
(**go/internal/foxtrot/blob_stores/multi.go**) that wraps a set of
already-initialised **BlobStore** values and presents them as a single
**BlobStore**. It supports two orchestration modes — **Mirror** and
**write-through** — selected at construction time.

Multi is consumed as a Go library by callers (notably **dodder**, where
it backs FDR-0015's multi-store read fallback). It is **not** a
config-file blob-store type today: there is no **init-multi** command,
no on-disk **!toml-blob_store_config-multi-v1** wire format, and no
CLI surface that resolves a Multi from a blob-store-id. A separate
config-type wrapper is being designed in **FDR-0009**
(**docs/features/0009-multi-store-config-type.md**); when it lands the
schema will live in that document or extend this page.

This page covers the primitive only.

# MODES

## Mirror

    multi, err := blob_stores.NewMulti(ctx).
        Mirror(storeA, storeB, ...).
        Build()

Mirror broadcasts every write to every child via **io.MultiWriter**.
Reads iterate children in registration order and return the first hit.
**AllBlobs** N-way merges the children's ordered sequences.

## Write-through

    multi, err := blob_stores.NewMulti(ctx).
        WriteTo(writeStore).
        Read(readA, readB, ...).
        ReadFill(true).
        Build()

Writes pin to a single **WriteTo** store; the **Read** stores are
never written to via Multi. Reads check the write store first, then
each read store in declared order. With **ReadFill(true)** (the
default for **NewMulti**), a successful read-store hit is also tee'd
into the write store as a cache fill — see **READFILL** below.

# BUILDER VALIDATION

**Build()** rejects every mis-shaped builder before returning a
**Multi**. The error message names the violation; the caller never
silently receives a degraded store.

**no mode selected**
:   Calling **Build()** on a fresh **NewMulti(ctx)** with neither
    **Mirror** nor **WriteTo** invoked returns
    *MultiBuilder: no mode selected*.

**mode confusion**
:   Calling both **Mirror** and **WriteTo** on the same builder, or
    invoking **Read** / **ReadFill** outside write-through mode, sets a
    **modeConfused** poison marker. Once poisoned, follow-up
    mode-selecting calls preserve the first violation rather than
    downgrading it. **Build()** returns
    *mode confusion; only one of .Mirror or .WriteTo is allowed*.

**WriteTo called more than once**
:   A second **WriteTo** call (even with the same store) sets a
    **modeWriteToTwice** poison. **Build()** returns
    *WriteTo called more than once; only one write store is allowed*.
    The rationale is that a helper-pre-configured **WriteTo(a)**
    followed by a caller's **WriteTo(b)** would silently drop a's
    plumbing.

**empty Mirror / no write store**
:   **Mirror()** with no varargs sets the mode but populates no
    children; **Build()** returns *Mirror: no stores given*. A
    **WriteTo** with a zero-value **BlobStoreInitialized** (no
    **BlobStore**) returns *WriteTo: no write store given*.

**write store also in read list**
:   In write-through mode, if any **Read** store has the same identity
    (path-id when both stores carry a **Path**, interface-value
    otherwise) as the **WriteTo** store, **Build()** returns
    *write store %q also appears in read list*.

# READER FALLBACK

**MakeBlobReader** iterates children in registration order and
returns the first store whose **HasBlob(id)** is true. In write-through
mode the **WriteTo** store is checked first, then each **Read** store
in declared order.

When no child claims the blob, **MakeBlobReader** returns
**blob_io.ErrBlobMissing{BlobId: id}** with the queried markl id
cloned into the error value. **ErrBlobMissing** does **not** carry the
list of stores it tried.

The current implementation is a two-pass per child: **HasBlob** is
called first, then **MakeBlobReader** on the same child. For local
filesystem stores this is two cheap stat-equivalents; for remote
backends (sftp, webdav, s3) it is two round trips. The single-pass
replacement — open-and-match on **ErrBlobMissing** — is tracked in
issue **#196** and will land before downstream consumers that pay
network latency per probe make Multi their default composition layer.

# WRITER SEMANTICS

## Mirror writers

**MakeBlobWriter** fans out via **io.MultiWriter** across every
child's **BlobWriter**. Any child write failure surfaces from the
parent **Write** / **ReadFrom** call. The Multi writer does **not**
implement partial-success roll-back: a failure mid-broadcast leaves
each successful child with its own state (typically a still-open
temp file the child's own cleanup logic governs).

**multiStoreBlobWriter.Close** propagates the first child **Close**
error it encounters.

**multiStoreBlobWriter.GetMarklId** asks every child writer for its
computed id and asserts the children agree via **markl.AssertEqual**.
A mismatch is treated as a contract violation (every child consumed
the same bytes via the shared **io.MultiWriter** and was created with
the same hash type) and **panics** rather than silently picking a
winner.

## Write-through writers

**MakeBlobWriter** delegates to the single **WriteTo** store. The
**Read** stores never have **MakeBlobWriter** called on them via the
parent.

# HASBLOB

**HasBlob** returns the OR of its children's answers. It iterates in
registration order (write store first in write-through mode) and
**short-circuits on the first hit** — children past the first
positive store are not probed.

# ALLBLOBS

**AllBlobs** N-way merges its children's ordered sequences. Each child
satisfies the **BlobStore** contract: ids arrive in ascending lex byte
order by **(format-id, raw bytes)** within a hash format.

**same-hash dedupe**
:   At every step the merge picks the lex minimum across live heads
    and yields it once; every head that compares equal to the minimum
    is advanced. Comparison uses **format-id + raw bytes**, **not**
    the blech32 **String** form. Same-hash entries that appear in
    multiple children collapse to a single yield.

**cross-hash pass-through**
:   Two ids in different hash formats have distinct format-ids and
    never compare equal. They pass through the merge as separate
    entries; no cross-hash dedupe is attempted.

**error semantics**
:   A head yielding **(nil id, non-nil err)** surfaces the error
    through the merged sequence and the merge advances past the error
    on the next pull, continuing with the head's remaining entries.
    A misbehaving head yielding **(nil id, nil err)** is silently
    skipped — feeding nil to the comparator would panic on a future
    refactor.

**caller-side cancellation**
:   Breaking out of the range loop on any yield (id or err) returns
    immediately; live heads are stopped via their **iter.Pull2** stop
    func in a deferred cleanup.

# READFILL (TEE-ON-READ)

In write-through mode, **ReadFill(true)** (the default for
**NewMulti(ctx)**) wraps a successful read-store hit in a
**teeBlobReader** that copies each **Read** chunk into a sink writer
constructed on the **WriteTo** store. The caller still receives the
full source bytes verbatim; the sink mirrors them out of band.

**default on**
:   **NewMulti(ctx)** sets **readFill = true**. Callers opt out via
    **.ReadFill(false)**.

**partial-read drain**
:   If the caller reads only part of the blob and abandons the
    reader, **flushAndCommit** drains the remainder through the tee
    before closing the sink so the cache fill is complete. The
    caller's partial read is unaffected.

**ctx.After fallback**
:   The tee registers a **ctx.After** callback that commits the sink
    if the caller never calls **Close**. **atomic.Bool done**
    serialises caller-**Close** against **ctx.After** so the sink is
    closed exactly once.

**sink-write failure tolerance**
:   A write error on the sink flips **sinkDead** and silences further
    tee writes; the source read continues uninterrupted and the
    caller still receives every byte. The sink's partial state is
    discarded at commit.

**writer-construction failure tolerance**
:   If **WriteTo.MakeBlobWriter** fails when the tee tries to
    construct the sink, Multi falls back to returning the read
    source's reader directly. The read still succeeds; the cache
    fill is skipped.

**same-hash digest mismatch**
:   When the expected id (from the read source) and the sink's
    computed id share a hash format, **flushAndCommit** asserts they
    are equal; a mismatch returns a detect-and-report error from
    **Close** (or the **ctx.After** path).

**cross-hash digest mapping**
:   When expected and sink-computed share no hash format,
    **flushAndCommit** registers the cross-hash pair on the
    **WriteTo** store via
    **BlobForeignDigestAdder.AddForeignBlobDigestForNativeDigest**
    when the store implements it, mirroring
    **CopyBlobIfNecessary**'s behaviour.

# DEGENERATE CASES

**Mirror with a single child**
:   **NewMulti(ctx).Mirror(only).Build()** succeeds. **HasBlob**,
    **MakeBlobReader**, **AllBlobs**, and **MakeBlobWriter** each
    delegate to the single child verbatim.

**Write-through with zero read stores**
:   **NewMulti(ctx).WriteTo(default).Build()** succeeds; the read
    fallback set is empty and **MakeBlobReader** returns
    **ErrBlobMissing** as soon as the write store does not have the
    blob.

**empty Mirror / WriteTo with no store**
:   Rejected at **Build()** — see **BUILDER VALIDATION**.

# INTERFACE CONFORMANCE

**Multi** satisfies **domain_interfaces.BlobStore** with a value
receiver:

    var _ domain_interfaces.BlobStore = blob_stores.Multi{}

Callers may pass **Multi** by value or by pointer. The struct holds
slices of **BlobStoreInitialized** and an **ActiveContext**; copying
the value shares those underlying children with the original.

The wrapper has no config of its own. **GetBlobStoreConfig**,
**GetDefaultHashType**, and **GetBlobIOWrapper** delegate to the
first child in Mirror mode and to the **WriteTo** store in
write-through mode. **GetBlobStoreDescription** synthesises
**multi/mirror(A,B,...)** in Mirror mode and
**multi/write-through(W=&lt;desc&gt;, R=&lt;desc&gt;, ...)** in
write-through mode.

# EXAMPLES

Mirror two stores so every write reaches both:

    storeA, _ := stores.Lookup("default")
    storeB, _ := stores.Lookup(".archive")

    multi, err := blob_stores.NewMulti(ctx).
        Mirror(storeA, storeB).
        Build()
    if err != nil {
        return err
    }

Write-through with cache fill on read:

    local, _ := stores.Lookup("default")
    archive, _ := stores.Lookup("/shared")

    multi, err := blob_stores.NewMulti(ctx).
        WriteTo(local).
        Read(archive).
        ReadFill(true). // default; shown for clarity
        Build()
    if err != nil {
        return err
    }

The second shape is the one downstream consumers (dodder's
multi-store read fallback) install in front of a local cache backed
by one or more remote archives.

# LIMITATIONS

**partial-success on Mirror writes**
:   A Mirror write that fails mid-broadcast leaves successful
    children with their own state; Multi does not roll those back.
    This is by design (#182) — the children own their durability
    contracts — but callers that need atomic mirror writes will need
    a higher-level coordinator.

**two-pass reader fallback**
:   **MakeBlobReader** pays **HasBlob + MakeBlobReader** per probed
    child today. Tracked in #196.

**no quota or budget**
:   The tee path has no per-write quota, byte budget, or sink-size
    cap. **ReadFill** trusts the **WriteTo** store's own admission
    policy.

**backend-unavailable fallback narrowness**
:   A backend that returns **blob\_io.ErrBlobStoreUnavailable** from
    **HasBlob** (as false) or **MakeBlobReader** (as the typed error)
    is treated as miss-equivalent and the next child is probed. The
    SFTP store routes SSH dial / handshake / auth failures through
    this path. Backends that surface unavailability via untyped
    errors (e.g. raw **\*net.OpError**, **net.Error.Timeout()**, or
    the documented SSH error strings) are also recognised by the
    classifier, but new backends SHOULD wrap unavailability in
    **ErrBlobStoreUnavailable** at the dial boundary for precision.
    See **blob\_io.IsBlobStoreUnavailable** and #209.

# SEE ALSO

**blob-store**(7), **markl-id**(7)

FDR-0009: Multi-store as a bonafide config type
(**docs/features/0009-multi-store-config-type.md**) — the planned
config-file wrapper around this primitive.

Issue **#196**: Multi reader two-pass to single-pass migration.

Issue **#209**: SFTP store unavailability fallback (resolved).
