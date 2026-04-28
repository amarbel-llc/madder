---
author:
-
date: April 2026
title: MADDER-INVENTORY-LOG(7) Madder \| Miscellaneous
---

# NAME

madder-inventory-log - per-blob audit log: wiring patterns for Go importers

# SYNOPSIS

    import "github.com/amarbel-llc/madder/go/pkgs/inventory_log"

    obs := inventory_log.WireDefault(ctx)         // ctx is errors.Context

    obs, cleanup := inventory_log.WireWithCleanup() // no errors.Context
    defer cleanup()

# DESCRIPTION

The **inventory_log** package writes a hyphence-wrapped NDJSON stream to
**$XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<id>.hyphence**, one file per
write session, with one record per blob publish. The format and lifecycle
are specified by ADR 0004 and the typed-write-log design plan.

Madder's CLI wires the log automatically. External Go consumers that embed
madder as a library import the **pkgs/inventory_log** facade and call one of
the wire helpers below. Both honor the **MADDER_INVENTORY_LOG=0** environment
variable for opt-out parity with the CLI.

# WIRING PATTERNS

## Default behavior, with errors.Context

The common case: code already running inside a futility command or otherwise
in possession of an **errors.Context**. **WireDefault** registers the
observer's **Close** as an After-hook on the context, so the trailing
hyphence buffer flushes when the context's **Run** completes.

    obs := inventory_log.WireDefault(ctx)

    // Hand to the blob store's env, the multi-store fan-out, etc.
    envDir.SetBlobWriteObserver(obs.(domain_interfaces.BlobWriteObserver))

The type assertion is safe: every concrete value **WireDefault** can return
(NopObserver{} when disabled, *FileObserver otherwise) implements
**domain_interfaces.BlobWriteObserver**.

## Default behavior, without errors.Context

For embedded libraries, test harnesses, and non-futility-driven entry points,
**WireWithCleanup** returns a cleanup func the caller defers.

    obs, cleanup := inventory_log.WireWithCleanup()
    defer cleanup()

    // Use obs as above.

The cleanup runs the observer's **Close**, which flushes hyphence's
**bufio.Writer** (currently 4096 bytes; partial-loss bound on hard kill is
that buffer size).

## Custom event types

The log is typed: importers register a **Codec** for any **LogEvent** they
want to emit, and the registered codecs share the **Global** registry across
the process. Reserved types (currently **blob-write-published-v1**) are
owned by inventory_log and panic on re-registration.

    package myapp

    import (
        "encoding/json"
        "github.com/amarbel-llc/madder/go/pkgs/domain_interfaces"
        "github.com/amarbel-llc/madder/go/pkgs/inventory_log"
    )

    type ImportEvent struct {
        Source string `json:"source"`
        Count  int    `json:"count"`
    }

    func (ImportEvent) LogType() string { return "myapp-import-v1" }

    var _ domain_interfaces.LogEvent = ImportEvent{}

    func init() {
        inventory_log.Global.Register(
            inventory_log.MakeCodec[ImportEvent](
                "myapp-import-v1",
                func(e ImportEvent) ([]byte, error) { return json.Marshal(e) },
                func(b []byte) (e ImportEvent, err error) {
                    err = json.Unmarshal(b, &e)
                    return
                },
            ),
        )
    }

    func runImport(obs inventory_log.Observer) {
        obs.Emit(ImportEvent{Source: "tape-7", Count: 1024})
    }

The **MakeCodec** generic binds the type-string at codec construction; the
encode and decode callbacks see typed values, no per-call assertions.

## Test-time CapturingObserver

Tests don't always want a real **FileObserver**. Implement **Observer**
directly with a slice-backed sink to assert on emitted events without
touching disk.

    type CapturingObserver struct {
        events []domain_interfaces.LogEvent
    }

    var _ inventory_log.Observer = (*CapturingObserver)(nil)

    func (c *CapturingObserver) Emit(e domain_interfaces.LogEvent) {
        c.events = append(c.events, e)
    }

    func (*CapturingObserver) RegisterCodec(inventory_log.Codec) inventory_log.Codec {
        return nil
    }

    func TestSomething(t *testing.T) {
        obs := &CapturingObserver{}
        runImport(obs)
        if len(obs.events) != 1 {
            t.Fatalf("expected 1 event, got %d", len(obs.events))
        }
    }

This boundary — wire format vs runtime behavior — is intentional: codec
registration is process-global so on-disk readers can decode every type;
runtime sinking is per-Observer so tests can substitute behavior without
touching the registry.

## Disabling the log

Two ways:

**MADDER_INVENTORY_LOG=0** — exact match (whitespace-tolerant); both
**WireDefault** and **WireWithCleanup** return **NopObserver{}** and
**WireWithCleanup**'s cleanup is a no-op. Library importers and madder CLI
users share this contract.

**Explicit NopObserver{}** — for tests that want to confirm "log disabled"
behavior deterministically without clobbering the env:

    obs := inventory_log.NopObserver{}

# LIFECYCLE

The **FileObserver**'s **Close** is required for graceful flush of the
hyphence wrapper's **bufio.Writer** and the underlying file. **WireDefault**
wires this for you (via **errors.ContextCloseAfter** on the supplied
context); **WireWithCleanup** returns the cleanup func for the caller to
defer. Both produce identical end-state.

The hyphence buffer is currently 4096 bytes. On a hard kill (SIGKILL, OOM)
the trailing partial buffer is lost; partial-record loss is bounded by that
size. ADR 0004 records this as the explicit non-goal — durability is
"best-effort, page-cache-survives-clean-exit." There is no **fsync** on the
emit path.

If a future caller needs strict durability, a **WithSync()** constructor
option is the contemplated extension point — see ADR 0004 follow-ups.

# DESCRIPTION SETTER

**\*FileObserver** also implements **DescriptionSetter**:

    type DescriptionSetter interface {
        SetDescription(s string)
    }

Calling **SetDescription** before any **Emit** stamps the supplied string
into every subsequent **BlobWriteEvent**'s **Description** field. The CLI
exposes this via **madder write --log-description '<intent>'**. Library
importers can do the same:

    obs := inventory_log.WireDefault(ctx)
    if setter, ok := obs.(inventory_log.DescriptionSetter); ok {
        setter.SetDescription("imported Q3 backup tapes")
    }

The type assertion naturally no-ops when the log is disabled (NopObserver
does not implement DescriptionSetter).

# FILES

**$XDG_LOG_HOME/madder/inventory_log/YYYY-MM-DD/<id>.hyphence**

:   per-session log file; **id** is an 8-hex-char random suffix to avoid
    collisions across processes. Deletion is safe and MUST NOT affect
    application correctness, per **xdg_log_home(7)**.

# ENVIRONMENT

**MADDER_INVENTORY_LOG**

:   Set to **0** (whitespace tolerant) to suppress the log. Library
    importers using **WireDefault** or **WireWithCleanup** observe the
    same disable contract as the madder CLI's **--no-inventory-log**
    flag.

**XDG_LOG_HOME**

:   Base directory for the inventory log; defaults to **$HOME/.local/log**
    when unset, per **xdg_log_home(7)**. Importers MAY override per
    invocation by setting this env var before calling **WireDefault**.

# SEE ALSO

**xdg_log_home(7)**, **hyphence(7)**, **blob-store(7)**

ADR 0004: docs/decisions/0004-blob-write-log-via-observer.md

Typed-write-log design: docs/plans/2026-04-26-typed-write-log-design.md
