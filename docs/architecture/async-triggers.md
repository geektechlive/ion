---
title: Async-trigger architecture
description: Internal design notes for the engine's webhook and scheduling subsystems and the shared spine that powers both.
---

# Async-trigger architecture

This document describes the engine-internal design of the async-trigger
subsystems: HTTP webhooks (D-011) and scheduled jobs (D-010). It is
written for future contributors working on the engine and the SDK
runtime, not for extension authors. For the extension-facing API see:

- [Webhooks SDK](../extensions/webhooks.md)
- [Scheduling SDK](../extensions/scheduling.md)

## Why one spine, two subsystems

Webhooks and schedules differ only in their trigger source:

- A webhook is triggered by an inbound HTTP request matching a
  registered (path, method) pair.
- A schedule is triggered by the tick loop noticing a job's
  `nextRun` has elapsed.

Everything downstream of "we have a fire for (kind, id)" is identical
across the two: resolve which session owns the host, build a fresh
extension Context for that session, send `engine/fire_async` into the
subprocess with the right payload shape, write the response.

So the engine implements:

1. A shared `asyncreg.Registry` (per host) holding declarations of
   both kinds.
2. A shared `Host.FireAsync(kind, id, ctx, payload, timeout)` that
   pins `ctx` as `currentCtx`, sends the RPC, and unpins on return.
3. Two trigger sources (`webhooks.Server`, `scheduling.Scheduler`)
   that read from the registry and call `FireAsync`.

## The registry

`engine/internal/asyncreg/registry.go`. A `*Registry` is owned by a
single `extension.Host` and stores the host's currently-registered
declarations under two kinds (`KindWebhook`, `KindSchedule`).

Operations:

- `Register(kind, decl, origin, vetoFunc) error` — adds the entry.
  The veto callback runs **outside the registry mutex** so a hook
  handler that recursively calls `Register`/`Deregister` does not
  deadlock. On success, publishes a `ChangeAdded` event to every
  subscriber.
- `Deregister(kind, id, notifyFunc) bool` — removes the entry,
  fires the notify callback (informational), publishes `ChangeRemoved`.
  Deregistration cannot be vetoed — letting one extension trap
  another extension's resources would be a footgun.
- `List(kind)`, `ByID(kind, id)`, `Count(kind)` — read accessors
  used by the dispatcher and scheduler.
- `Subscribe(kind, buffer)` — returns a channel that receives
  `ChangeEvent` on every successful add/remove. Slow subscribers
  drop events rather than stall the registry.
- `Reset(kind, notify)` — wipes every entry of a kind, used by the
  host on respawn before re-committing the new subprocess's init
  payload.

A per-kind cap (default 256, configurable via `New(cap)`) prevents
runaway registration storms. Hitting the cap returns
`ErrCapExceeded`; the host's RPC layer surfaces it as a -32000 error.

## The fire envelope

`engine/internal/extension/host_fire_async.go`. `Host.FireAsync`
sends an `engine/fire_async` JSON-RPC request to the subprocess with
this envelope:

```go
type AsyncFirePayload struct {
    Kind       string      `json:"kind"`        // "webhook" | "schedule"
    ID         string      `json:"id"`          // route path or job id
    SessionKey string      `json:"sessionKey"`  // for SDK runtime correlation
    Payload    interface{} `json:"payload"`     // kind-specific shape
}
```

For webhooks, `Payload` is a flat map containing `method`, `path`,
`url`, `query`, `headers`, `body`, `remote`. The SDK runtime exposes
`req.json<T>()` and `req.text()` as sugar over the body string.

For schedules, `Payload` is `{firedAt: <RFC3339>}` — minimal because
the handler is invoked with the current context and decides for itself
what to do.

Crucially, `FireAsync` pins the provided `ctx` as `currentCtx` for
the duration of the call:

```go
prev := h.currentCtx.Load()
h.currentCtx.Store(ctx)
defer h.currentCtx.Store(prev)
```

This is **the single piece that retires the cache-a-ctx workaround
that pre-D-010/D-011 harnesses used**. Inside the SDK runtime's
`engine/fire_async` handler, the ctx the handler sees is freshly
built by `extcontext.NewExtContext`, and every `ext/*` RPC it makes
(`dispatchAgent`, `sendPrompt`, `emit`, `setPlanMode`, …) resolves
through the engine's existing `host_rpc.go` dispatch with that
pinned ctx. Same path as a real hook handler.

## Session resolution

`session/async_lifecycle.go:buildAsyncContextResolver`. Given a host,
return a fresh `extension.Context` for the session the host is bound
to.

The resolution rule is intentionally simple:

1. `host.SessionKey()` returns the session key captured at load time
   by `Manager.wireHostAsync`.
2. The manager looks up the session under `m.mu`. Missing session
   returns an error; both subsystems treat this as
   `engine_async_fire_dropped` with `asyncReason: "no_session"`.
3. `m.newExtContext(s, key)` builds a fully-wired ctx via the
   existing `extcontext.NewExtContext` path. The async handler gets
   the full SDK surface — `dispatchAgent`, `sendPrompt`,
   `setPlanMode`, `getContextUsage`, `searchHistory`, every callback
   that real hook ctx has.

The webhook server and the scheduler both call this resolver. Same
function shape but with nominally different types
(`webhooks.SessionResolver` vs `scheduling.SessionResolver`); the
manager wraps as needed.

## Lifecycle hook pipeline

Four hooks: `webhook_registered`, `webhook_deregistered`,
`schedule_registered`, `schedule_deregistered`. The `*_registered`
variants are veto-capable; the `*_deregistered` are observational.

Wiring (from registration call to handler) goes:

1. **Caller** invokes either:
   - `host.CommitPendingAsyncDecls()` (init-time bulk), or
   - `Host.RegisterWebhookDecl` / `RegisterScheduleDecl` (runtime RPC).
2. **Host** calls `asyncreg.Register(kind, decl, origin, vetoFunc)`.
   The vetoFunc closure invokes `Host.fireLifecycleHook(event, info)`.
3. **Host.fireLifecycleHook** delegates to the wired
   `async.onLifecycleHook` callback (set by the session manager via
   `Host.SetOnLifecycleHook`).
4. **Manager** (`session/async_lifecycle.go`) builds a fresh ctx and
   calls `SDK.FireWebhookRegistered(ctx, info)` (or
   `FireScheduleRegistered`).
5. **SDK** iterates the registered hook handlers. For subprocess
   extensions, the handler is the forwarder registered by
   `registerAsyncRegistrationVetoForwarder` in
   `hook_forwarders_async.go`. The forwarder sends `hook/<name>` to
   the subprocess and decodes the `{block, reason}` response into a
   `*AsyncRegistrationVeto`.
6. **fireAsyncRegistrationVeto** collects every result and resolves
   the last explicit Block to a non-nil error.
7. **asyncreg.Register** sees the error, rolls back the entry, and
   returns the error to the caller.

A subtle point: steps 4–7 run in a goroutine spawned by
`host_rpc_async.go:rpcRegisterWebhook` (and the three siblings).
This is essential because step 5 sends a JSON-RPC request to the
subprocess, but the read loop that would receive the response is the
same goroutine that's currently processing the `ext/register_webhook`
RPC inbound from the subprocess. Without the goroutine, the veto
fire would deadlock at the 30s RPC timeout.

## Server / scheduler lifecycle

Both `webhooks.Server` and `scheduling.Scheduler` are
**Manager-level singletons**, allocated lazily on first
`ensureAsyncSubsystems` call. Sharing across sessions means the
engine never tries to bind two webhook listeners on the same port,
and the single tick loop polls every host in every session.

Lifecycle:

- `Start()` opens the listener (webhooks) / launches the tick
  goroutine (scheduler).
- `AddHost(h)` adds a host whose registry the subsystem reads.
- `RemoveHost(h)` drops a host at session teardown.
- `Stop()` graceful shutdown — `http.Server.Shutdown` for webhooks,
  channel-close for scheduler.

The subsystems auto-start when a host with non-empty registrations
joins the pool, controlled by `Manager.startWebhookServerIfNeeded` /
`startSchedulerIfNeeded`. A future iteration may auto-stop when the
last entry leaves; today they stay running for the manager's lifetime
once started.

## Respawn

When a host's subprocess dies and the manager respawns it via
`Host.Respawn`, the per-host `asyncreg.Registry` survives (it lives
on the Host, not the subprocess). The new subprocess re-runs
`parseInitResult` which re-populates the `pendingInit*` buffers; the
session manager's respawn flow calls:

```go
host.ResetAsyncRegistrations()    // wipe stale entries (fires deregistered hooks)
m.commitHostInitAsyncDecls(...)   // commit the new init payload
```

Without `ResetAsyncRegistrations`, every re-commit would hit
`asyncreg.ErrDuplicate` from stale entries.

Dynamic registrations from the prior subprocess are not restored on
respawn — the extension is responsible for re-issuing them in
`session_start` if needed. This matches the existing agent-spec
model.

## Token / predicate resolution

Auth secrets (`WebhookAuth.tokenRefName`) and schedule enable
predicates (`ScheduleJob.enabledRefName`) are resolved lazily through
`engine/resolve_token` / `engine/resolve_predicate` RPCs the engine
sends to the subprocess at fire time. The extension's
`() => string | () => bool` callback runs inside the subprocess —
secrets never sit in engine memory longer than the auth check
requires.

Both RPCs use a short 5-second timeout. Extensions that need to
fetch secrets from a remote secret store should cache externally and
return the cached value synchronously.

## Observability event routing

`Manager.buildAsyncEventEmitter` routes `engine_*_*` events back to
the right session by walking each session's extension group and
matching `(asyncKind, asyncId)` against its hosts' registries. Events
without an `asyncId` (e.g. `engine_async_fire_dropped` from a
no-resolver path) emit to every active session as a fallback so the
event is never silently dropped.

A future iteration could thread the session key through the
`AsyncFirePayload` envelope so the emitter has direct routing instead
of doing a per-event lookup. For the MVP, walking active sessions on
each event is O(small) and not on a hot path.
