---
status: moved
moved-to: amarbel-llc/hyphence docs/man.7/hyphence.md
author:
-
date: April 2026
title: HYPHENCE(7) Madder \| Miscellaneous
---

# NAME

hyphence - text-based metadata + body serialization format (moved to the
hyphence repository)

# DESCRIPTION

The hyphence (hyphen-fence) format and its documentation moved out of madder
into their own repository, **amarbel-llc/hyphence**, so the format can host
multiple implementations (Go, with Rust to follow) of one canonical wire
format. Madder still uses hyphence for blob-store metadata, but no longer owns
its specification.

The canonical, maintained reference manual lives at:

<https://github.com/amarbel-llc/hyphence/blob/master/docs/man.7/hyphence.md>

and the normative specification (RFC 0001) at:

<https://github.com/amarbel-llc/hyphence/blob/master/docs/rfcs/0001-hyphence.md>

This page is a redirect stub kept so existing **hyphence**(7) cross-references
resolve. See the madder git history for the prior in-tree content. Tracked in
madder issue #253.

# SEE ALSO

**markl-id**(7), **blob-store**(7), **madder-inventory-log**(7).
