---
title: Scheduling SDK
description: Register daily, weekly, and interval jobs from an Ion extension; the engine fires them on cadence with a fresh ctx.
sidebar_position: 11
---

# Scheduling SDK

Extensions register scheduled jobs through `ion.schedule.daily(...)`,
`ion.schedule.weekly(...)`, and `ion.schedule.interval(...)`. The
engine runs a 1-second tick loop (off by default; auto-starts when
any job is registered) and dispatches each job's handler with a
freshly-built `ctx`.

Inside the handler the full SDK surface is available — `dispatchAgent`,
`sendPrompt`, `emit`, `setPlanMode`, `getContextUsage`, and
`searchHistory` all work normally, the same path hook handlers use.

See [D-010 Scheduling SDK](https://github.com/dsswift/ion/tree/main/.analysis)
for the design decision.

## Quick start

```ts
import { createIon } from '../sdk/ion-sdk'

const ion = createIon()

// Fire once per day at 09:00 in New York time.
ion.schedule.daily({
  id: 'morning-summary',
  time: '09:00',
  tz: 'America/New_York',
  handler: async (ctx) => {
    await ctx.dispatchAgent({
      name: 'summariser',
      task: 'Compose today\'s morning summary',
    })
  },
})

// Fire every 30s.
ion.schedule.interval({
  id: 'inbox-poll',
  intervalMs: 30_000,
  handler: async (ctx) => {
    // ... poll an external feed, dispatch agents on new items.
  },
})

// Fire every Monday at 18:00 local.
ion.schedule.weekly({
  id: 'weekly-digest',
  dayOfWeek: 'monday',
  time: '18:00',
  handler: async (ctx) => { /* ... */ },
})
```

Static registration is the most common shape; the SDK queues the
declaration and ships it to the engine in the `init` handshake.

## Configuration

The engine's scheduler is OFF by default. It auto-starts when any
extension registers a job. `engine.json` exposes a few tuning knobs:

```jsonc
{
  "scheduling": {
    "defaultTz": "America/New_York",  // applied to daily/weekly when job omits tz
    "fireTimeoutMs": 60000,             // 60s default handler timeout
    "catchUpEnabled": true              // run missed daily/weekly fires on startup
  }
}
```

Last-run markers persist to `~/.ion/scheduler/<host>_<job>.json` so
daily/weekly catch-up survives engine restarts. The directory and
files are recreated on demand; deleting them is safe and only loses
the catch-up dedup signal.

## Job shapes

### Daily

```ts
ion.schedule.daily({
  id: string,                              // required, stable
  time: string,                             // "HH:MM" 24-hour
  tz?: string,                              // IANA tz; default = engine default
  timeoutMs?: number,                       // override fire timeout
  enabled?: () => boolean | Promise<boolean>, // skip predicate; see below
  handler: (ctx) => Promise<void> | void,
})
```

### Weekly

```ts
ion.schedule.weekly({
  id: string,
  time: string,
  dayOfWeek: 'monday' | 'tuesday' | 'wednesday' | 'thursday' | 'friday' | 'saturday' | 'sunday',
  tz?: string,
  timeoutMs?: number,
  enabled?: () => boolean | Promise<boolean>,
  handler: (ctx) => Promise<void> | void,
})
```

### Interval

```ts
ion.schedule.interval({
  id: string,
  intervalMs: number,        // >= 1000 (scheduler ticks at 1s granularity)
  timeoutMs?: number,
  enabled?: () => boolean | Promise<boolean>,
  handler: (ctx) => Promise<void> | void,
})
```

Sub-second intervals are rejected at registration time — the
scheduler's 1s tick would alias unpredictably.

## Enable predicate

The optional `enabled` callback is invoked at each fire opportunity.
Return `false` to skip — the engine emits `engine_schedule_skipped`
with `reason: 'disabled'` and advances `nextRun`.

```ts
ion.schedule.interval({
  id: 'work-hours-only',
  intervalMs: 60_000,
  enabled: () => {
    const h = new Date().getHours()
    return h >= 9 && h < 18
  },
  handler: async () => { /* ... */ },
})
```

The predicate is invoked through an `engine/resolve_predicate` RPC
into the subprocess at fire time, so it can read any state local to
the subprocess (env vars, in-memory caches, etc.).

## Dynamic registration and the handle

Each `ion.schedule.*` call returns a `ScheduleHandle`:

```ts
interface ScheduleHandle {
  id: string
  unregister(): Promise<void>
}
```

Static and dynamic registration share the same surface. Calls made
inside a hook handler or tool issue `ext/register_schedule` /
`ext/deregister_schedule` RPCs.

```ts
ion.on('session_start', async (ctx) => {
  const job = await ion.schedule.interval({
    id: `poll-${ctx.sessionKey}`,
    intervalMs: 5000,
    handler: async () => { /* ... */ },
  })
  // ... later:
  await job.unregister()
})
```

## Lifecycle hooks

```ts
ion.on('schedule_registered', (ctx, info) => {
  // info: { kind: 'schedule', id: string, origin: 'init' | 'runtime', decl: ScheduleJob }
  if (info.id.startsWith('test_')) {
    return { block: true, reason: 'test jobs disabled in prod' }
  }
})

ion.on('schedule_deregistered', (ctx, info) => {
  log.info('schedule removed', { id: info.id })
})
```

`schedule_registered` is **veto-capable**; `schedule_deregistered`
is observational only. Veto rejections surface the reason to the
caller via the registration RPC error.

## Observability events

| Event | Fires when |
|---|---|
| `engine_schedule_fired` | Handler returned successfully |
| `engine_schedule_skipped` | Enable predicate returned false, or session unavailable |
| `engine_schedule_failed` | Handler threw or timed out |
| `engine_schedule_registered` | Registration committed |
| `engine_schedule_deregistered` | Deregistration committed |
| `engine_async_fire_dropped` | Fire dropped before reaching the handler |

Every event carries `asyncKind: "schedule"`, the `asyncId` (job id),
and `asyncDurationMs` where applicable.

## Catch-up on restart

For daily/weekly jobs only, when the engine starts and discovers a
scheduled slot was missed while it was down, the catch-up sweep
schedules a fire ~30s after startup (a stagger so 10 missed jobs
don't all fire at once). Interval jobs do **not** catch up; they
simply fire at `now + intervalMs`.

Catch-up reads the last-run marker from `~/.ion/scheduler/` to decide
whether a missed slot is genuinely missed (no marker after the slot
time) or already handled (marker after the slot time).

## In-process dedup

A `sync.Map` in the engine prevents overlapping fires of the same
job: if a handler is still running when its next tick arrives, the
tick logs a skip and waits for the previous fire to complete. This
guarantees a single in-flight invocation per job, regardless of
handler latency.

Cross-subprocess arbitration (multiple engine processes sharing the
same job set) is intentionally out of scope — the engine runs as a
single process today.

## Respawn

If the extension subprocess crashes and the engine respawns it, the
new subprocess's `init` payload is the authoritative declaration set.
The previous registrations are wiped and the new ones re-register
through the same lifecycle pipeline. Last-run markers persist on
disk so dedup survives.

**Dynamic registrations from the prior subprocess are NOT restored.**
If you need a dynamically-added job to survive respawn, install a
`session_start` hook that re-issues the registration; this is the
same pattern agent specs use.

## Plan mode

If a session is in plan mode when a job fires, the handler runs
normally. Plan mode is an agent-loop constraint, not a session-wide
quiet mode. Handlers that want to defer-while-in-plan-mode can check
at entry:

```ts
handler: async (ctx) => {
  const [planMode] = ctx.getPlanMode()
  if (planMode) return // skip this tick
  // ... normal work
},
```

## Migration

If your extension currently runs its own `setInterval` polling and
caches a `ctx` workaround to dispatch agents, the migration is
mechanical:

```ts
// before
let cachedCtx: IonContext | undefined
ion.on('session_start', (ctx) => { cachedCtx = ctx })
setInterval(async () => {
  if (!cachedCtx) return
  await cachedCtx.dispatchAgent(...) // cached-ctx workaround
}, 30_000)

// after
ion.schedule.interval({
  id: 'inbox-poll',
  intervalMs: 30_000,
  handler: async (ctx) => {
    await ctx.dispatchAgent(...) // ctx is fresh per fire
  },
})
```

The cached-ctx workaround is no longer needed: the engine builds
`ctx` fresh on every fire through `extcontext.NewExtContext`, the
same path hook handlers already use.
