---
title: "ADR-005: Plan-Mode End-of-Turn Discipline and Sparse-Reminder Override Symmetry"
description: Engine adds negative-example callouts to both plan-mode prompts, extends the reminder gate to fire on mature-session turn-1 runs, and exposes PlanModeSparseReminder as a parallel override to PlanModePrompt.
sidebar_position: 5
---

# ADR-005: Plan-Mode End-of-Turn Discipline and Sparse-Reminder Override Symmetry

## Status

Accepted

## Date

2026-05-25

## Context

Session `1779681912646-285484dee4df` exhibited a plan-mode drift pattern:
the model wrote a prose question ("Let me know if you'd like changes") or
a prose summary, made exactly one `Edit` tool call against the plan file,
then ended its turn — never calling `AskUserQuestion` or `ExitPlanMode`.
The desktop showed a normal assistant message with no question card and no
plan card.

Engine log analysis confirmed three reinforcing causes:

1. **No negative examples in the prompt.** `buildPlanModePrompt` told the
   model "end turns with AskUserQuestion or ExitPlanMode" but did not list
   the forbidden prose anti-patterns. Claude Code's equivalent prompt does
   (Claude Code `src/utils/messages.ts:3290`) and those bullet points are
   precisely what the model needs to internalize the rule.

2. **Sparse reminder never fired on single-turn runs.** The reminder gate
   checked `turn > 1`. In mature plan-mode conversations every "what's
   next?" round is a one-turn run — turn > 1 never fires, and the model
   anchors on its own recent pattern (prose + Edit + end_turn). The engine
   logs for the broken runs showed zero `reminder injected` lines.

3. **Full prompt was buried.** The initial full plan-mode prompt was ~220
   messages back. With each new round adding two more messages, the model
   anchored on recent assistant behavior instead of the distant full prompt.

The engine already had a full override path for `PlanModePrompt` (via
`RunOptions.PlanModePrompt`, the `plan_mode_prompt` hook, and the engine
default). The **sparse reminder** had zero override path — a harness that
set `RunOptions.PlanModePrompt` still got Ion's default sparse reminder
injected every 5 turns, potentially contradicting the custom framing.

## Decision

### Fix 1: Add negative anti-pattern examples to the full prompt and sparse reminder

In `buildPlanModePrompt` (the full plan-mode system prompt), append a
`## Forbidden Prose Patterns` block directly under `## Turn Behavior`
listing the specific drift phrases:

> Phrases like "Is this plan okay?", "Should I proceed?", "How does this
> plan look?", "Any changes before we start?", "Let me know if you'd like
> changes", "Does the plan look good?", "Should I go ahead?" — these MUST
> use ExitPlanMode or AskUserQuestion. Never write them as prose.

In `buildPlanModeSparseReminder`, append a one-liner version of the same
callout so it appears in the per-turn reminder too.

This mirrors Claude Code's established pattern for this exact problem.

### Fix 2: Fire the reminder on turn 1 of mature sessions

Replace the `turn > 1` gate in the runloop with a call to a new peer
function `shouldInjectPlanModeReminderForRun(turn, lastReminderTurn,
conversationMessageCount int) bool`:

- Turn 1 with message count ≤ `planModeFirstTurnReminderThreshold` (8):
  skip — the full prompt was just injected at plan-mode entry.
- Turn 1 with message count > threshold: inject — the full prompt is far
  back in context; this is the mid-plan follow-up case that was broken.
- Turn 2+: delegate to the existing `shouldInjectPlanModeReminder`
  throttle (unchanged behavior for multi-turn runs).

The threshold of 8 means "at least 4 back-and-forth message pairs have
occurred before this run started", which reliably distinguishes a fresh
plan-mode entry from a mature planning conversation.

### Fix 3: Expose PlanModeSparseReminder as a parallel override

Add `RunOptions.PlanModeSparseReminder string` (additive `omitempty`)
mirrored on `ClientCommand.PlanModeSparseReminder`. When non-empty, the
engine uses this string verbatim for every per-turn reminder injection
instead of calling `buildPlanModeSparseReminder`. When empty, the engine
falls back to the default.

Extend `PlanModePromptResult` (the `plan_mode_prompt` hook return type)
with a `SparseReminder string` field. The hook fires as a secondary
override when `RunOptions.PlanModeSparseReminder` is empty.

Precedence (highest to lowest):
1. `RunOptions.PlanModeSparseReminder` (wire field, set per `send_prompt`)
2. `plan_mode_prompt` hook's `SparseReminder` return field
3. `buildPlanModeSparseReminder(planFilePath)` (engine default)

The resolved override is cached on `activeRun.planModeSparseReminderOverride`
once at run setup, identical in structure to how `planFilePath` is
pre-cached.

### Fix 4: Desktop constants + settings.json power-user knobs

The desktop ships its reference sparse reminder as `PLAN_MODE_SPARSE_REMINDER`
in `desktop/src/main/prompt-pipeline.ts` (parallel to `ENTER_PLAN_MODE_DESCRIPTION`).
It is forwarded on every engine-tab and CLI prompt dispatch.

Power users override either constant by setting
`desktop.planModePrompt` or `desktop.planModeSparseReminder` in
`~/.ion/settings.json`. The desktop reads these keys at session start and
substitutes them for the built-in constants. Settings.json only, no
renderer UI, per ADR-004's posture.

## Consequences

### Positive

- The model sees the forbidden prose patterns listed explicitly next to
  the positive rule on every turn it takes a tool action in plan mode.
- Mature single-turn rounds — the most common case mid-plan — now get the
  reminder on turn 1 instead of never.
- The full prompt and sparse reminder are symmetrically overridable. A
  harness that customizes one can also customize the other, so Ion's
  default framing doesn't silently contradict the harness's custom prose.
- All changes are additive `omitempty` fields. Third-party harnesses that
  don't set any of the new fields see identical wire bytes and behavior.

### Negative

- `buildSystemPrompt` now takes an additional `*activeRun` parameter
  (nil-safe) to enable caching the hook's sparse reminder on the run at
  setup time. All existing call sites (tests, the main runloop) updated.
- The `FirePlanModePrompt` and `OnPlanModePrompt` signatures grew a third
  return value. All call sites updated; the build breaks if any site is
  missed.

### Symmetry argument

The full plan-mode prompt (`PlanModePrompt`) and the sparse reminder
(`PlanModeSparseReminder`) are now parallel override targets with
identical three-layer precedence. This symmetry is by design: both are
policy prose that belongs in the harness (per ADR-001/004); both should
be equally accessible to a power user who wants to customize either or
both.

## Related

- [ADR-001](./001-engine-vs-harness.md) — the parent boundary principle.
- [ADR-004](./004-enter-plan-mode-prose-in-harness.md) — the parallel
  `EnterPlanModeDescription` precedent this ADR extends to the sparse
  reminder.
- `engine/internal/backend/plan_mode_prompt.go` — engine side: prompt
  builders, gate functions, constants.
- `desktop/src/main/prompt-pipeline.ts` — desktop reference prose:
  `ENTER_PLAN_MODE_DESCRIPTION` + `PLAN_MODE_SPARSE_REMINDER`.
- [Plan mode prose overrides](../../sessions/lifecycle.md#plan-mode-prose-overrides) — full precedence documentation.
- [client-commands.md#send_prompt](../../protocol/client-commands.md#send_prompt) — wire fields.
- [settings-json.md](../../configuration/settings-json.md#desktop-power-user-overrides) — power-user override keys.
