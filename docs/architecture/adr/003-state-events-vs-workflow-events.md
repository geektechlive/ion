---
title: "ADR-003: State Events vs Workflow Events"
description: An event reports either a state transition or a workflow proposal. Never both.
sidebar_position: 3
---

# ADR-003: State Events vs Workflow Events

## Status

Accepted

## Date

2026-05-24

## Context

The engine's `engine_plan_mode_changed` event originally fired on two semantically different triggers, both producing the same wire shape:

1. **State transitions** — when the harness called `SetPlanMode`, when a run started with `PlanMode: true`, when plan mode was aborted, or when the user-approval chokepoint flipped the mode back to auto. These are *facts about the session's state*: plan mode is now on, or plan mode is now off.

2. **Workflow proposals** — when the model called the `ExitPlanMode` tool. This is *not* a state change; it's a request from the model that the user be given an opportunity to approve exiting plan mode. The actual state change is deferred to the user-approval chokepoint, which may never arrive if the user dismisses the proposal.

The conflation produced a race the desktop had to filter around: `engine_plan_mode_changed{Enabled: false}` would fire on the model's `ExitPlanMode` call, and the desktop would receive it before the user had any opportunity to approve, with the side effect of flipping its mode dropdown to "auto" prematurely. The desktop had to add a guard that ignored `Enabled: false` events whose origin was the model's tool call, distinguishable only by inspecting `permissionDenials` on the subsequent `task_complete` event.

A workaround landed on the engine side in commit `7a955793` (`fix(desktop): defer plan exit to user approval chokepoint`): the engine stopped emitting `engine_plan_mode_changed{Enabled: false}` on the `ExitPlanMode` tool call entirely. That fixed the race but left consumers without any first-class signal that the model had proposed an exit. Consumers had to infer the proposal from the presence of an `ExitPlanMode` entry in `task_complete.permissionDenials` — which is a permission-denial record, not a workflow signal, and which arrives only at run end.

The deeper problem is that **the original event was carrying two different semantic responsibilities at the same time**. Stopping one trigger fixed the symptom; clarifying the contract fixes the cause.

## Decision

**An event reports either a state transition or a workflow proposal. Never both.**

### `engine_plan_mode_changed` — state-only

Fires *only* on confirmed state transitions:

- Harness called `SetPlanMode(true)` or `SetPlanMode(false)`.
- Run started with `PlanMode: true` in `RunOptions`.
- Plan mode was aborted (engine-internal failure path).
- The user-approval chokepoint approved the exit and called `SetPlanMode(false)`.

The model calling `ExitPlanMode` **does not** fire this event.

### `engine_plan_proposal` — workflow-only

A new event discriminated by `Kind`:

- `"exit"` — the model has called `ExitPlanMode`. Carries `PlanFilePath` and `PlanSlug` directly so consumers don't have to scrape `permissionDenials.toolInput` to recover them.

The discriminator is open for future kinds:

- `"enter"` — could fire when the model calls `EnterPlanMode` (currently auto-approved, no proposal stage).
- `"amend"` — could fire when the model proposes an amendment to an in-progress plan.

Consumers must switch on `Kind` and treat unknown kinds as forward-compatible no-ops. The `kind`-discriminator pattern matches the precedent set by `SystemInjectInfo.Kind`; it's the engine's idiomatic way of grouping "events that share a wire shape but differ in semantic role."

### The card-render path

The `ExitPlanMode` permission denial continues to flow through `engine_status.permissionDenials` and `task_complete.permissionDenials` as today. The desktop's existing permission-card render path keys off `permissionDenied.tools[]` and already handles `ExitPlanMode`. The new `engine_plan_proposal` event becomes the additive, primary, first-class workflow signal; the permission denial becomes a tool-permission record and not a workflow side-channel. A follow-up de-dupe can collapse the permission-card render path onto the new event once consumers have migrated.

## Consequences

### Positive

- Each event has one semantic role. Consumers reading the event stream can reason about state and workflow independently.
- The race that motivated the desktop's `Enabled: false` filter no longer exists at the contract level. Filtering becomes the kind of guard you write defensively, not the kind you have to write to make the feature work.
- The discriminator gives future plan-mode work (enter proposals, amend proposals) a clean expansion path without inventing more event types.
- The pattern generalizes: any future event the engine is tempted to fire on "the model proposed X" vs "X happened" should choose one role per event type and prefer a discriminated variant if the data shape is shared.

### Negative

- Breaking change for consumers that listened to `engine_plan_mode_changed{Enabled: false}` to detect the model's `ExitPlanMode` call. The engine removed the `ExitPlanMode` trigger in commit `7a955793`; the `engine_plan_proposal` event is the replacement. Shipped as `feat(engine)` rather than `feat!(engine)` per the user's accepted breaking-change scope; this ADR is the authoritative record of what changed and why.
- Two events to listen for instead of one. Consumers that genuinely care about both must subscribe to both. Acceptable: that's the cost of unconflated semantics.

### Future considerations

- If `engine_plan_proposal` grows additional kinds (`enter`, `amend`), the same un-conflation discipline applies inside the event: each kind is a workflow signal, and a *new* state-event surface is added if the engine needs to report the state change distinctly.
- The state/workflow split is a special case of the broader snapshot/incremental contract documented in [`docs/architecture/agent-state.md`](../agent-state.md). The agent-state doc reads as the singular example today; this ADR makes the pattern explicitly general.

### Forbidden by extension

Following from this ADR, the engine-grounding doc gains a new "Forbidden (breaking)" bullet: stopping the emission of an existing event on one of its established triggers is a breaking change, even when the wire shape is unchanged. Splitting one event into two with cleaner semantics is acceptable; silently dropping a trigger is not.

## Related

- [ADR-001](./001-engine-vs-harness.md) — the parent boundary principle.
- [ADR-002](./002-engine-vs-harness-early-stop.md) — sibling application of the same boundary to the early-stop feature.
- [ADR-004](./004-enter-plan-mode-prose-in-harness.md) — sibling application of the same boundary to the `EnterPlanMode` tool description.
- [`docs/architecture/agent-state.md`](../agent-state.md) — the canonical example for event semantics (snapshot vs incremental); this ADR extends the framing to state vs workflow.
- [`docs/sessions/lifecycle.md`](../../sessions/lifecycle.md) — plan-mode lifecycle, updated to reflect the new event split.
- [`docs/engine-grounding.md`](../../engine-grounding.md) — house rules including the new "stop emitting on an established trigger" forbidden bullet.
