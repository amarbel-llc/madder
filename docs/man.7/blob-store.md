---
author:
-
date: April 2026
title: BLOB-STORE(7) Dodder \| Miscellaneous
---

# NAME

blob-store - dodder content-addressable blob storage

# DESCRIPTION

A blob store is a content-addressable storage backend that holds the raw data
(blobs) referenced by dodder objects. Each blob is identified by a markl ID
derived from its content digest (see **markl-id**(7)). A dodder repository can
have multiple blob stores configured simultaneously, each with a unique store
ID.

Blob stores are managed by the **madder** utility or via dodder's
**blob_store-** prefixed commands.

# BLOB-STORE-IDS

Every blob store has an ID that determines its storage scope. The first
character of the ID is an optional prefix indicating the backing XDG
location:

**(unprefixed)**
:   XDG user store. Located under **$XDG_DATA_HOME/madder/blob_stores/**
    (typically **~/.local/share/madder/blob_stores/**). Example: **default**

**.**
:   CWD-relative store. Located under
    **$PWD/.madder/local/share/blob_stores/**, rooted in the current working
    directory rather than the ancestor directory where **.madder/** was
    found. Example: **.archive**

**/**
:   XDG system store. Located under system-wide XDG data directories.
    Example: **/shared**

**%**
:   XDG cache store. Located under
    **$XDG_CACHE_HOME/madder-cache/blob_stores/** (typically
    **~/.cache/madder-cache/blob_stores/**). Purgeable — managed by the
    **madder-cache**(1) utility. Example: **%scratch**

**\_**
:   Unknown location. Reserved for stores whose filesystem root is
    determined by configuration rather than the XDG scheme. Example:
    **\_custom**

**~**
:   XDG user store — accepted on parse for backward compatibility but
    never emitted; equivalent to the unprefixed form.

The ID portion after the prefix may contain only **\[a-zA-Z0-9_-\]**.

Two blob-store-ids that share a name but differ in prefix refer to
**different stores** at different filesystem locations. For example,
**default** and **.default** are distinct stores.

Unprefixed names resolve to XDG user stores by default, but several
commands (**write**, **pack-blobs**) consult the filesystem first: if a
file of the same name exists in CWD, the argument is treated as a file
path. To target a store unambiguously in a directory that may contain a
like-named file, use an explicit prefix (**~mystore** or **\_mystore**) or
the full disambiguating form.

# STORE TYPES

## Local Hash-Bucketed

The default store type. Blobs are stored as individual files in a directory
tree bucketed by digest prefix (similar to Git's object storage). Created with
**madder init**.

## Inventory Archive

Packs multiple blobs into indexed archive files for efficient storage and O(1)
lookups via a fan-out table. Supports optional delta compression. Three format
versions exist (v0, v1, v2); use **madder init-inventory-archive** for the
current version.

Archive management commands: **madder pack** consolidates loose blobs into
archives, **madder pack-list** lists archive files, and **madder pack-cat-ids**
lists blob digests within archives.

## SFTP

Remote blob store accessed over SSH/SFTP. Two initialization modes:

**madder init-sftp-explicit**
:   Explicit host, port, user, and key path.

**madder init-sftp-ssh_config**
:   Connection parameters resolved from **~/.ssh/config** host entries.

Both support **-discover** to detect an existing remote store's configuration
from its directory structure.

## Pointer

A store that delegates to another store by reference. Created with **madder
init-pointer**. The pointer store does not hold blobs itself but redirects reads
and writes to the target store.

# INLINE STORE SWITCHING

Several madder commands accept positional arguments that can be either data
arguments (file paths, markl IDs) or blob-store-ids. When an argument parses
as a blob-store-id, it switches the active store for all subsequent
arguments.

For file-accepting commands (**write**, **pack-blobs**), the shared helper tries
to open the argument as a file first, falling back to blob-store-id parsing.
For digest-accepting commands (**cat**, **has**), blob-id parsing is tried
first since markl IDs are unambiguous (they start with a hash algorithm
name).

Example:

    madder write file1.txt .archive file2.txt file3.txt

This writes **file1.txt** to the default store, then switches to **.archive**
and writes **file2.txt** and **file3.txt** there.

# CONFIGURATION

Blob store configurations are persisted as hyphence-encoded files (see
**hyphence**(7) when available) in the repository's **.madder/** directory. Use
**madder info-repo** to inspect the current configuration and **madder
init-from** to initialize a store from an existing configuration file.

# SEE ALSO

**madder**(1), **markl-id**(7)
