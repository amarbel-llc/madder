---
status: exploring
date: 2026-05-06
decision-makers: Sasha F
---

> **Status note:** captured almost verbatim from the conversation that
> produced it; needs user review before promotion to `proposed`.

# Use typed panics as the failure-propagation channel for ctx-less BlobStore methods

## Context and Problem Statement

Madder's `BlobStore` interface exposes a mix of methods: some carry an
explicit error return (`MakeBlobReader`, `MakeBlobWriter`, `AllBlobs`),
some do not (`HasBlob`, `GetBlobIOWrapper`, `GetDefaultHashType`,
`GetBlobStoreConfig`, `GetBlobStoreDescription`). Backends like
`remoteSftp` can fail at the no-error methods because their state is
fetched lazily from a remote host, and the interface gives them no
in-band channel for that failure. Issue #134 surfaced this when an
unreachable SFTP store crashed `madder-mcp serve` via dewey's
`ctx.Cancel(err)` "structured throw" pattern propagating outside of
any `Run` frame.

The fix that landed (commits `9b74186` + `b3e0ae9`) had SFTP panic
directly with the underlying error and added two recover boundaries
in madder-mcp (`tryOpenInStore` per-store; `ReadResource`
per-request). That works, but leaves an open question: **is
panic-as-channel a workaround for a missing capability in the
interface, or a deliberate design choice that we should formalise?**

This ADR captures the conversation that produced the answer so future
contributors do not re-derive it from scratch.

## Decision Drivers

* **Function-coloring cost of error returns.** Adding `error` to
  every `BlobStore` method forces every layer in a call tree to
  inherit the error-handling discipline of its leaves. Middleware
  that just orchestrates becomes noise: `if err := ...; err != nil {
  return err }` ×N. Languages with effects/exceptions don't pay this
  cost; intermediate code can stay polymorphic over what its inner
  functions might raise.
* **Context-dependent failure semantics.** The same low-level event
  ("SSH dial failed") means structurally different things at
  different call sites: in `openBlob`'s walk-the-stores loop it is
  "skip this store, try the next"; in a future `fsck` run it is
  "this is a verification failure"; in a debug introspection tool it
  might be "log it and continue." If the leaf method returns
  `error`, the *interpretation* of the error has to be repeated at
  every call site. We want the leaf to *describe what happened* and
  the surrounding scope to *decide what it means*.
* **Go's idiomatic baseline.** Stdlib's convention is "method
  returns `error` if any plausible implementation could fail at it."
  By that rule, `HasBlob` etc. should return `error`. We are
  consciously diverging.
* **Practical surface size.** Madder's BlobStore consumer set is
  small (madder's CLI commands, madder-mcp, future internal
  embeddings, dodder via the dewey pkgs façade). The cost of a
  documented unenforced convention is bounded; the cost of
  threading `error` through every call site is not.
* **Existing dewey machinery.** dewey already commits to
  panic-as-control-flow: `Cancel(err)` is a structured throw caught
  by `runRetry`. Adding a parallel discipline for backend errors
  layers cleanly on top.

## Considered Options

1. **Add `error` returns to every BlobStore method that can plausibly
   fail.** Threads context through call sites; matches Go stdlib
   idiom. Touches every backend, every caller, every test stub.
2. **Keep no-error signatures; backends call `ctx.Cancel(err)` to
   surface failures.** dewey's pre-#134 convention. Works inside a
   `Run` frame; breaks for long-lived embeddings.
3. **Keep no-error signatures; backends `panic` with a typed value;
   callers `recover` at known boundaries and assign meaning per
   context.** A poor-man's algebraic effect system in Go.
4. **Hybrid: `MustX` convenience wrappers around an error-returning
   interface.** Stdlib's `regexp.Compile` / `regexp.MustCompile`
   pattern. Caller chooses the ergonomic shape per call site.

## Decision Outcome

Chosen option: **3 (typed panics with named recover boundaries)**,
because it preserves middleware ergonomics, lets the *call-site
context* decide what a failure means rather than baking that decision
into the leaf, and aligns with established discipline from Common
Lisp condition systems and modern algebraic effects (Koka, Eff,
OCaml 5 effect handlers). We accept that Go's lack of static effect
typing means the discipline is enforced by convention and code
review rather than by the compiler.

### Consequences

* **Good, because** middleware between leaf and handler does not need
  to declare or propagate the failure channel. Defers handle cleanup;
  signatures stay clean.
* **Good, because** the *handler* — not the leaf — decides what a
  failure means in context. `tryOpenInStore` treats backend
  unreachability as "skip"; `fsck` treats the same panic as a
  verification failure. The leaf signals; the caller interprets.
* **Good, because** stack traces are free. A panic carries the call
  stack at the throw site, which is often more useful than a wrapped
  error chain.
* **Good, because** the discipline composes with dewey's existing
  `ctx.Cancel` / `runRetry` panic mechanism. CLI commands sit inside
  a `Run` frame and unhandled backend panics abort the command —
  the same shape as `ctx.Cancel`-driven aborts.
* **Bad, because** Go gives no static guarantee that handlers exist.
  A backend panic with no surrounding `recover` crashes the process.
  Compiler does not flag this.
* **Bad, because** Go gives no type discipline on the panic payload.
  Every recover boundary does its own type switch. A typo in a
  payload type silently turns into "fall through and re-panic."
* **Bad, because** Go has no resumable exceptions / restarts. Recover
  can only unwind, not resume from the throw site. Some patterns
  (Common Lisp's `restart-case`) are not expressible.
* **Bad, because** the convention is unenforced and easily violated
  by a backend author who adds a new no-error method without
  documenting which panic types it raises.

### Confirmation

* The BlobStore interface comment names the convention and the
  panic-payload contract.
* Each long-lived embedding of `BlobStoreEnv` (madder-mcp today;
  future library mode tomorrow) establishes recover boundaries at
  the granularity its caller-context needs.
* CI runs the bats SFTP integration suite (33 tests, real local
  sshd) and the manufactured "unreachable SFTP" smoke test in
  `just debug-mcp-resources` — both exercise the typed-panic path
  end-to-end.
* Ideally, a small `Handle[T]` helper standardises the
  recover + type-switch shape so every boundary site uses the same
  scaffolding (out of scope for this ADR; tracked as #134 follow-up).

## Pros and Cons of the Options

### 1. Add `error` returns everywhere

* Good, because it matches Go stdlib idiom — `io.Reader.Read`,
  `fs.FS.Open`, `net.Dial` all return `error` for the same reason
  (some implementation might fail).
* Good, because failure is statically legible: every caller of
  `HasBlob` has to acknowledge it can fail, and the compiler
  enforces it.
* Good, because debugging is grep-able: error messages flow through
  named return paths.
* Bad, because it forces function coloring across the entire call
  tree. Every layer between leaf and consumer has to thread `error`,
  even when the layer has no business interpreting failure.
* Bad, because it bakes a single failure interpretation into the leaf
  signature. The same `HasBlob` returning `(bool, error)` can't
  cleanly say "this is a skip in openBlob, a fault in fsck"; the
  interpretation moves to every call site.
* Bad, because the rewrite is large. Touches every backend, every
  caller, every test stub. Not small enough to land alongside other
  work.

### 2. `ctx.Cancel(err)` from backends (status quo pre-#134)

* Good, because it composes with dewey's existing structured-throw
  machinery and its `Run` frame catch.
* Good, because backends don't need to know about per-call ctx
  threading — they hold the env's ctx and signal against it.
* Neutral, because in a CLI context with a single Run frame around
  the whole command, this works fine and is invisible to most
  consumers.
* Bad, because it conflates "abort this run" (control flow) with
  "report this error" (data). A backend that wants to report an
  error has no choice but to also abort everything sharing the
  context.
* Bad, because long-lived embeddings (madder-mcp serve, future
  library mode) sit outside a `Run` frame; the throw escapes and
  crashes the host. This is exactly the bug #134 captured.
* Bad, because the panic payload carries dewey's
  `ContextStateSucceeded` sentinel rather than the underlying
  error, due to a separate dewey TODO at `GetState` (closed-Done is
  always reported as Succeeded). Error messages mislead.

### 3. Typed panics with named recover boundaries (chosen)

* Good, because middleware stays clean. Layers between leaf and
  handler don't have to declare or propagate the failure channel.
* Good, because the *handler* assigns meaning. The same low-level
  event (SSH dial fail) becomes a "skip" in openBlob and a "fault"
  in fsck — same leaf, different handlers.
* Good, because it composes with dewey's existing panic
  conventions. CLI commands inside a Run frame absorb backend
  panics the same way they absorb `ctx.Cancel` panics.
* Good, because it captures stack traces and lets `recover` decide
  whether to log, wrap, suppress, or re-panic.
* Neutral, because the convention is documented but not statically
  enforced. Code review and CLAUDE.md notes carry the weight.
* Bad, because there is no compile-time guarantee that every
  panic-raising leaf has a covering recover. A new long-lived
  embedding can ship with the same bug madder-mcp had pre-#134.
* Bad, because Go has no resumable exceptions; recovers only
  unwind. Patterns like CL's `invoke-restart 'use-cached-value` are
  not expressible.

### 4. `MustX` wrappers around an error-returning interface

* Good, because it gives callers the ergonomic shape they want at
  the call site (panic-style or error-style) while keeping the
  interface honest.
* Good, because it's the stdlib pattern (`regexp.MustCompile` /
  `regexp.Compile`, `template.Must`, `time.Date` /
  `time.MustParse`).
* Neutral, because it doubles the API surface. Each method gets two
  shapes.
* Bad, because the leaf still has to commit to *one* interpretation
  of failure (the error type). The "context decides meaning" win of
  option 3 is lost.
* Bad, because it doesn't fix function coloring — the underlying
  interface is still error-returning, so middleware that wants to
  call the error variant pays the same threading cost as option 1.

## More Information

### The lineage

The pattern chosen here is not novel. It traces back to **Common
Lisp's condition system** (1980s, formalised in CLtL2). A function
`signal`s a condition (typed value); the *outer dynamic scope*
establishes handlers via `handler-case` / `handler-bind`. The
handler decides what the condition means in this context. Crucially,
handlers can offer **restarts** — they can resume the signaler at
known recovery points, not just unwind. The signaler describes what
happened; the handler decides what it means and whether to continue.

The modern revival is **algebraic effects** (Koka, Eff, OCaml 5
effect handlers). Same shape, statically typed, with proper
compiler support. The leaf says "I might perform effect E"; the
surrounding handler says "I handle E by doing X." Different
handlers in different scopes give different semantics for the same
leaf. Effect systems were literally invented to fix the
function-coloring critique that motivates this ADR.

### Java's typed exceptions and why they don't fit

Java's checked exceptions are statically declared at the leaf
(`throws BackendUnreachableException`), and every intermediate
layer has to either catch or re-declare the throws clause. This is
Go's error-return problem with a different syntax — the leaf bakes
in the exception type and middleware pays. What we want is the
*type discipline* of Java's checked exceptions but with the
*handler-decides-meaning* property of CL conditions: i.e.,
algebraic effects. Java does not offer this; Go doesn't either, but
Go's panic-recover with disciplined typed payloads gets closer than
Java's `throws`.

### What Go offers and what it lacks

Used disciplined-ly, Go's panic-recover is a poor-man's effect
system:

- Leaf panics with a typed value.
- Middleware passes through.
- Boundaries `recover()` and type-switch the value.

What Go lacks:

1. **No static guarantee that handlers exist.** Compiler doesn't
   help.
2. **No type discipline on the panic payload.** Each recover does
   its own switch; payload-type typos silently fall through.
3. **No restart machinery.** Recovers can only unwind.

### Where this lands for BlobStore

The existing interface signatures stay. Backends that can fail at
no-error methods *panic with a typed value carrying the underlying
error*. Long-lived consumers establish recover boundaries at the
granularity their caller-context wants. The convention is documented
on the interface and enforced by code review.

The follow-up work (separate from this ADR) is:

1. Document the convention on the BlobStore interface comment in
   `go/internal/0/domain_interfaces/blob_store.go`.
2. Define a small set of exported panic-payload types (e.g.
   `BackendUnreachableError`) in a known package and document on
   each interface method which panic types it may raise.
3. Sketch a `Handle[T]` helper that standardises the recover +
   type-switch shape so every boundary site uses the same scaffolding.

### References

- Issue [#134](https://github.com/amarbel-llc/madder/issues/134) —
  symptom report and the corrected analysis comment that motivated
  this ADR.
- Commit `9b74186` — `remoteSftp.initializeOnce` panics directly
  with the underlying error.
- Commit `b3e0ae9` — madder-mcp adds `tryOpenInStore` and
  `ReadResource` recover boundaries.
- Common Lisp the Language, 2nd Ed., Ch. 29 (Conditions). Steele,
  G., 1990. — The canonical reference for the
  signal-handler-restart shape.
- Koka language documentation, "Algebraic Effects." Leijen, D. —
  Modern statically-typed effect handlers; the closest existing
  language match for what this ADR describes informally.
- OCaml 5 effect handlers (RFC and OCaml manual). — Production
  language adoption of algebraic effects.
- Pretnar, M. "An Introduction to Algebraic Effects and Handlers"
  (2015). — Tutorial-style introduction; good for the conceptual
  framing.
- Dewey context machinery: `purse-first/libs/dewey/bravo/errors/context.go`,
  particularly `(*context).Cancel` (line 259) and `runRetry`
  (line 134). The `GetState` "TODO identify the right terminal
  state" comment (line 94) is a separate concrete bug worth filing.
