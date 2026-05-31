---
title: Ion Meta Extension
description: Authoring partner for Ion harnesses in any language. Three-mode design (teach / improve / build), nine introspection tools, mode and knowledge specialist sub-agents, deterministic git-gate, bundled canonical docs.
sidebar_position: 10
---

# Ion Meta Extension

Ion Meta is the authoring partner for Ion harnesses. It exposes the full SDK and hook surfaces as live tools (`ion_list_hooks`, `ion_list_sdk_methods`, `ion_read_doc`), provides mode and knowledge specialist sub-agents, scaffolds new extensions / agents / skills to disk, ships the entire canonical Ion documentation set bundled for offline `ion_read_doc` access, and gates write-class tool calls behind a deterministic check that the target is inside a git working tree.

It auto-installs to `~/.ion/extensions/ion-meta/` via `make engine`.

## Modes

ion-meta supports three top-level user intents. The orchestrator classifies each user turn and dispatches the right specialist — there is no mode toggle and no slash command.

| Mode | When it fires | Specialist |
|------|---------------|------------|
| **Teach** | You ask a question about Ion ("how does X work?", "what's the difference between Y and Z?", "how do I emit `engine_agent_state` from Python?") | `ion-tutor` (or a knowledge specialist if narrow) |
| **Improve** | You point at an existing harness on disk in any language | `extension-improver` |
| **Build** | You ask for a new harness ("build me…", "scaffold a new…") | `extension-builder` |

When intent is ambiguous, the orchestrator asks exactly one clarifying question before dispatching.

ion-meta helps with **any Ion harness in any language** — TypeScript, Go, Python, Rust, C#, Swift, shell, anything that can read/write JSON-framed lines on stdin/stdout. The TypeScript and Go SDKs are conveniences over the wire protocol; the protocol is the contract.

## Model tiers

ion-meta agents declare abstract tiers (`fast`, `standard`) in their frontmatter — never concrete model ids. The engine resolves these against your `~/.ion/models.json` `tiers` section; unset tiers fall back to your session default. The orchestrator uses `fast` (intent classification, runs every turn); the specialists use `standard`. The `reasoning` tier stays reserved for explicit user invocation when a question is genuinely architectural.

ion-meta ships no model opinions. If you do nothing, every agent resolves to your session default. If you want per-role optimisation, map `fast` and `standard` in `~/.ion/models.json` to whatever models your providers offer.

## Loading

Either run it directly:

```bash
ion prompt --extension ~/.ion/extensions/ion-meta/index.ts \
  "Scaffold an extension named foo into /tmp/foo"
```

…or register it as a desktop engine profile so the New Tab → Engine picker (desktop and iOS) exposes it. Desktop Settings → **Engine** → **+** (Add) → set Name to `ion-meta` and Extensions to `/Users/<you>/.ion/extensions/ion-meta/index.ts`.

## Specialist agents

| Agent | Kind | Focus |
|-------|------|-------|
| `orchestrator` | Router | Classifies user intent and dispatches one specialist. Never performs work itself. Tier: `fast`. |
| `ion-tutor` | Mode | Teacher. Explains Ion concepts grounded in canonical docs. Read-only. |
| `extension-improver` | Mode | Pair programmer. Reads an existing harness in any language and proposes/applies targeted improvements. |
| `extension-builder` | Mode | Code generator. Greenfields a new harness end-to-end; verifies via language-appropriate check. |
| `extension-architect` | Knowledge | Extension structure, manifest, build pipeline, JSON-RPC protocol |
| `agent-designer` | Knowledge | Agent `.md` files, hierarchies, parent-child model, discovery |
| `skill-author` | Knowledge | Skill `.md` authoring |
| `hook-specialist` | Knowledge | Every engine hook, payload type, the five return patterns |
| `testing-guide` | Knowledge | Integration tests, MockProvider/MockBackend, JSON-RPC subprocess harness |
| `orchestration-designer` | Knowledge | Dispatch flows, `engine_agent_state` snapshots, the capability surface |

## Tools

All tools are deterministic. They never paraphrase the SDK or hook list from memory — the catalog is parsed from the live SDK source.

### `ion_scaffold`

Generate an extension / agent / skill. With `targetDir` writes files to disk; without, returns the templates inline for preview.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | enum (`extension` \| `agent` \| `skill`) | yes | What to scaffold |
| `name` | string | yes | Name of the item |
| `targetDir` | string (absolute path) | no | Parent directory. Extensions go under `<targetDir>/<name>/`; agent/skill files go directly under `<targetDir>/` |

### `ion_validate_agent`

Validate agent markdown frontmatter. Checks required fields (`name`, `description`), the `tools` shape (must be a YAML inline array), the `model` id prefix, and the body (warns when empty or starts with TODO). When `filePath` is supplied, also checks the `parent` reference against sibling agents.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `content` | string | yes | Full markdown content of the agent file |
| `filePath` | string | no | Absolute path of the agent .md (enables parent-reference check) |
| `peers` | string[] | no | Explicit peer override |

### `ion_validate_manifest`

Schema-check an `extension.json` body. Hard-errors on unknown top-level keys (the engine rejects them), missing `name`, and ill-typed `external` / `engineVersion`.

| Field | Type | Required |
|-------|------|----------|
| `content` | string | yes |

### `ion_list_hooks`

Return the hook catalog. Generated from `HookPayloadMap` in the SDK's `types.ts` — never stale. When the bundled canonical `docs/hooks/reference.md` is present, joins each hook with its payload type and use-case.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `category` | string | no | Filter to one category |
| `name` | string | no | Exact hook lookup |

### `ion_list_sdk_methods`

Return the full `IonContext` method surface with signatures and one-line descriptions. Sourced from `types.ts`.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | no | Exact method lookup |
| `contains` | string | no | Case-insensitive substring filter on name/signature |

### `ion_list_extensions`

Enumerate extensions under `~/.ion/extensions/` (or `dir`). For each: entry point, manifest fields, bundled agents.

| Field | Type | Required |
|-------|------|----------|
| `dir` | string | no |

### `ion_inspect_extension`

Parse an extension and report its registered hooks, tools, commands, imports, manifest fields, and `external` deps.

| Field | Type | Required |
|-------|------|----------|
| `path` | string | yes |

### `ion_read_doc`

Read a canonical Ion doc bundled with the extension. Allow-listed to four namespaces; rejects path traversal and absolute paths.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | no | Relative path under `extensions/`, `hooks/`, `agents/`, or `architecture/` |
| `list` | boolean | no | When `true`, return the index of available docs instead of reading one |

### `ion_typecheck_extension`

Run `esbuild --bundle` against an extension and surface parse / import errors. Returns the first ~20 error lines. Note: esbuild does not typecheck — for full TS coverage the author still runs `tsc --noEmit` separately.

| Field | Type | Required |
|-------|------|----------|
| `path` | string | yes |

## Hooks ion-meta registers

| Hook | Purpose |
|------|---------|
| `session_start` | Emit the initial `engine_agent_state` snapshot listing every dispatchable specialist as idle |
| `before_prompt` | Inject the generated persona as the system-prompt addition |
| `tool_call` | Deterministic git-gate; refuses `Write` / `Edit` / `Bash` / `ion_scaffold` when target is outside a git working tree |
| `agent_start` | Mark the dispatched specialist as `running` in the snapshot; emit `engine_working_message` |
| `agent_end` | Mark the specialist back to `idle`; preserve last task as `lastWork` |
| `capability_discover` | Emit telemetry advertising ion-meta's capability set |
| `capability_match` | Route free-form intent to a capability id; emits a custom event |
| `on_error` | Surface caught errors via `engine_notify` |
| `session_end` | Emit `agents: []` to wipe the panel per the snapshot contract |

The `engine_agent_state` emissions follow the snapshot contract documented in [agent-state](../architecture/agent-state.md): complete snapshots replace, never merge.

## Deterministic git-gate

The `tool_call` hook is the canonical example of ion-meta's deterministic-seams design ([ADR-006](../architecture/adr/006-deterministic-seams-and-probabilistic-judgment.md)). It refuses every `Write`, `Edit`, `Bash`, or `ion_scaffold` call whose target is outside a git working tree, returning `{ block: true, reason }`. The LLM cannot override the gate.

Why: reversibility. The improver and builder both write to user files. If those files aren't under git, the user has no `git diff` to review and no `git checkout` to back out. By requiring the target be inside a git repo, every edit is auditable and revertible — ion-meta does not maintain its own backup/journal machinery.

Detection: the gate walks up from the target path looking for `.git/` (directory) or `.git` (file pointer for git worktrees). Stops at the filesystem root or `~`, whichever comes first. Cached per-resolved-ancestor within a session.

Edge cases:
- A `Bash` call evaluates the session `cwd` (the inbound cwd is the deterministic signal at gate time).
- An `ion_scaffold` call where `targetDir` doesn't exist yet evaluates the *parent* directory (the new files will land there).
- Scaffolding into `~/.ion/extensions/<new>/` requires `~/.ion/extensions/` to be a git repo (or `git init` it). This is a one-time setup, not a permanent obstacle.
- When ion-meta is editing its own source (inside the Ion engine repo), the gate passes because the engine repo is itself a working tree. The persona-level "no engine source edits" rule layers above the gate as a domain restriction.

The gate is unconditional and non-bypassable. No trusted-directories allowlist, no env-var escape hatch, no LLM-arg override.

## Greeting

The `session_start` welcome (a static markdown block emitted as `engine_harness_message` when no on-disk conversation file exists for the session key) is tagged with `metadata.dedupKey: 'ion-meta:welcome'`. The desktop renderer suppresses repeated harness messages with the same `dedupKey` within a single engine-instance scrollback, so the welcome appears at most once per tab even if `session_start` fires several times before any user turn (e.g. an app restart with no message typed — the filesystem check still reads "fresh" because there's no zero-turn conversation to persist). The filesystem-based freshness check is the pre-emit optimization; the renderer is the source of truth. See [engine_harness_message well-known metadata keys](../protocol/server-events.md#well-known-metadata-keys-for-engine_harness_message) for the convention.

## Capability surface caveat

The capability hooks are observation-only from a TypeScript extension. The engine's TS forwarder treats them as string-returning, but the Go-side dispatcher expects structured `[]Capability` / `*CapabilityMatchResult` returns. A TS string return is discarded. ion-meta uses these hooks for telemetry; real routing belongs in a Go extension or in `ctx.registerAgentSpec` side effects.

## Drift detection

Run `/ion--update-ion-meta-extension` from any Claude Code session in the Ion repo to re-audit ion-meta against the live SDK / docs. The command is read-only and produces a structured markdown drift report.

## Implementation notes

Ion Meta uses the canonical `createIon()` SDK pattern (`engine/extensions/sdk/ion-sdk`). Catalog and SDK introspection happen at runtime by reading the SDK's `types.ts` — the catalog cannot drift from the engine's contract. When the SDK source is unreadable (rare), the extension falls back to a baked-in snapshot.

Source: `engine/extensions/ion-meta/`. Architecture is documented in the extension's own [README](https://github.com/your-org/ion/blob/main/engine/extensions/ion-meta/README.md).
