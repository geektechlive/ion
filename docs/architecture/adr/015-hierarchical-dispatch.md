---
title: "ADR-015: Hierarchical Dispatch"
description: Orchestrator dispatches leads, leads dispatch specialists. Each tier distills context, answers questions from the tier below, and surfaces only genuinely unanswerable questions to the user.
sidebar_position: 15
---

# ADR-015: Hierarchical Dispatch

## Status

Accepted

## Date

2026-06-29

## Context

The ion-dev extension evolved from a flat model (orchestrator dispatches any agent) to a hierarchical model (orchestrator dispatches department leads, leads dispatch their own specialists). This ADR documents the architecture, the engine mechanisms that enable it, and the harness policy that governs information flow between tiers.

The core tension: the engine is deliberately opinionless about organizational hierarchy (per ADR-001). It provides generic mechanisms (dispatch, depth tracking, question routing). The harness (ion-dev extension) applies policy on top of those mechanisms to create a distillation chain with answer-or-escalate semantics.

## Decision

### (1) Three-tier distillation chain

The dispatch hierarchy has three tiers:

- **Tier 0 (Orchestrator):** Holds full user context. Distills the user's intent into a scoped task and dispatches the appropriate department lead. Never forwards the user's raw message as a task.
- **Tier 1 (Lead):** Holds domain expertise and the distilled task from the orchestrator. Breaks it into specialist-scoped subtasks and dispatches via `dispatch_specialist`. Each subtask is 1-3 sentences, verb-first, with clear deliverables.
- **Tier 2 (Specialist):** Receives a narrowly scoped implementation task. Executes it. Has the highest bar for asking questions.

Each tier compresses context for the tier below. The specialist never sees the user's original message; it sees the lead's distillation of the orchestrator's distillation.

### (2) Answer-or-escalate elicitation

When a dispatched agent calls AskUserQuestion, the question does not go directly to the user. Instead:

- A **specialist's question** routes to its dispatching lead. The lead answers from its richer domain context if possible. If the lead cannot answer, it escalates to the orchestrator.
- A **lead's question** routes to the orchestrator. The orchestrator answers from the full user context if possible. Only genuinely unanswerable, outcome-defining questions surface to the user.

The bar for escalation rises at each tier:
- Orchestrator: answers most questions from user context. Surfaces to user when the answer requires information or a decision only the user holds.
- Lead: answers most questions from domain knowledge. Escalates to orchestrator when the question requires cross-department context or user-level intent.
- Specialist: resolves ambiguity itself whenever possible. Asks the lead only when genuinely blocked (cannot proceed without the answer) and the wrong assumption would waste the work.

### (3) Plan mode at every tier

Any tier can run in plan mode. When dispatched with `planMode: true`, the agent produces a plan file instead of implementing. The orchestrator can dispatch a lead in plan mode, review the plan, then re-dispatch for implementation. A lead can similarly dispatch a specialist in plan mode for complex subtasks.

Plan mode is an engine mechanism (the `planMode` field on `DispatchAgentOpts`). The decision of when to use it is harness policy.

### (4) Per-lead team scoping

When a lead is dispatched, the extension activates a team scoped to that lead's specialists (via `activateLeadTeam` in the ControlRoom). This replaces the default leads-only team with a dynamic team containing the lead plus its declared specialists. The scoping ensures `dispatch_specialist` resolves to the lead's own specialists, not to leads from other departments.

The activation happens in the `before_agent_start` hook: when the dispatched agent is a lead with declared specialists, the extension calls `activateLeadTeam` before the lead's first turn.

### (5) Engine mechanism vs. harness policy

The engine provides three mechanisms that enable hierarchical dispatch. None of them encode organizational hierarchy:

**Registry id-addressing (08118121).** Every dispatch is registered by a unique dispatch id. The registry tracks running dispatches across depth levels. This enables the harness to recall, steer, or continue any dispatch regardless of its position in the hierarchy. The engine does not know or care that dispatch A is "a specialist under a lead."

**Child-question block/resume (29fa0e27).** When a dispatched child calls AskUserQuestion, the engine blocks the child's run and surfaces the question to the dispatcher via the `OnChildQuestion` callback. The dispatcher can answer (the child resumes) or let it escalate. The engine provides the block/resume mechanism; the harness decides who answers.

**Depth tracking.** Each dispatch inherits `currentDepth + 1`. The engine enforces a configurable `MaxDispatchDepth` cap (default 3, allowing depths 0, 1, 2). The extension reads depth from the dispatch context and uses it for UI rendering (indentation, hierarchy visualization) and the `agentDepth` computation in the ControlRoom.

The harness policy layer (ion-dev extension) adds:
- The distillation chain (tier-aware instruction content in agent .md files)
- Per-lead team scoping (ControlRoom.activateLeadTeam)
- Answer-or-escalate judgment (each tier's instructions define its escalation bar)
- Dynamic parent/depth derivation for UI state (ControlRoom.findParent, ControlRoom.agentDepth)

## Rationale

### Alternative (a): flat dispatch (orchestrator dispatches all agents) -- rejected

Flat dispatch worked when the team was small. As the team grew, the orchestrator became a bottleneck: it had to hold specialist-level context for every domain, and every specialist question surfaced directly to the user. The distillation chain reduces the orchestrator's cognitive load (it only thinks in terms of departments) and the user's question load (most questions are answered by intermediate tiers).

### Alternative (b): engine-level hierarchy (engine knows about leads/specialists) -- rejected

Encoding organizational hierarchy in the engine violates ADR-001. The engine would need to know about agent types, parent-child relationships, and escalation rules. These are harness concerns that change with organizational structure. The engine provides the dispatch/depth/question mechanisms; the harness maps them to its organizational model.

### Alternative (c): specialist direct-to-user questions -- rejected

Letting specialist questions bypass the dispatch chain and go directly to the user defeats the purpose of the hierarchy. The lead has domain context that can answer most specialist questions without user involvement. The answer-or-escalate pattern ensures the user only sees questions that genuinely require their input.

## Consequences

### Positive

- **User sees fewer questions.** Most dispatched-agent questions are answered by an intermediate tier that has the domain context to answer them. Only genuinely unanswerable questions surface.
- **Specialists get faster answers.** The lead is co-resident in the dispatch context and can answer immediately, rather than waiting for the user to notice and respond.
- **Clean separation of engine mechanism and harness policy.** The engine provides generic primitives. The harness composes them into an organizational model. Changing the org structure (adding tiers, changing escalation rules) requires no engine changes.
- **Per-lead team scoping prevents cross-dispatch confusion.** A dispatched dev-lead sees only dev specialists. A dispatched docs-lead sees only docs specialists. No risk of a lead accidentally dispatching another department's specialist.

### Negative / obligations

- **Instruction maintenance cost.** The distillation chain and escalation bar are encoded in agent .md instruction files. Changes to the organizational model require updating instruction files across all affected tiers.
- **Deeper stacks increase latency.** A specialist question that escalates through lead to orchestrator to user traverses three block/resume boundaries. This is the correct trade-off (most questions don't escalate that far), but pathological cases add latency.
- **MaxDispatchDepth constrains hierarchy depth.** The default cap of 3 allows orchestrator (0), lead (1), and specialist (2). Deeper hierarchies require increasing the cap, which increases resource consumption.

## Related

- [ADR-001](./001-engine-vs-harness.md) -- foundational. This ADR is a direct application of engine-executes/harness-decides. The engine provides dispatch, depth, and question mechanisms; the harness composes them into organizational hierarchy.
- [ADR-014](./014-dispatch-conversation-identity.md) -- extends. Per-dispatch conversation isolation (ADR-014) is a prerequisite for hierarchical dispatch: each lead and specialist gets its own conversation context, preventing cross-bleed between dispatch tiers.
- [ADR-006](./006-deterministic-seams-and-probabilistic-judgment.md) -- applies. The answer-or-escalate judgment at each tier is a probabilistic decision (can I answer this?). The block/resume mechanism is deterministic. The seam is clean: deterministic routing, probabilistic judgment.
- [ADR-008](./008-wire-event-naming-and-ownership.md) -- consistent. Engine events emitted during hierarchical dispatch (engine_dispatch_start, engine_dispatch_end, engine_agent_state) follow the wire naming convention.
