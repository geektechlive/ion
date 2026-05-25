# ion-meta

Authoring partner for Ion harnesses — extensions, agents, skills, hooks, and any program that speaks the engine's JSON-RPC wire. Ships as a TypeScript SDK extension with a generated hook catalog, introspection / scaffolding / validation tools, mode and knowledge specialist sub-agents, the full canonical Ion documentation bundled for offline lookup, and a deterministic git-gate that refuses write-class tool calls outside a git working tree.

## Design philosophy

Ion's defining property is the ability to mix **deterministic code** with **probabilistic inference** at well-defined hook seams (see [ADR-006](../../../docs/architecture/adr/006-deterministic-seams-and-probabilistic-judgment.md)). ion-meta is built around that principle: the agents make probabilistic judgments (which specialist to dispatch, what edit to propose, how to phrase a response); the harness enforces deterministic invariants (the git-gate, the snapshot contract, ground-truth verification via tool calls). When ion-meta teaches Ion design, it teaches this framing.

## Modes

ion-meta supports three top-level user intents. You don't pick a mode — the orchestrator classifies your prompt and routes to the right specialist. Same conversation, no slash commands, no mode toggle.

| Mode | When it fires | Specialist |
|------|---------------|------------|
| **Teach** | You ask a question about Ion ("how does X work?", "what's the difference between Y and Z?", "how do I emit engine_agent_state from Python?") | `ion-tutor` (or a knowledge specialist if narrow) |
| **Improve** | You point at an existing harness on disk in any language | `extension-improver` |
| **Build** | You ask for a new harness ("build me…", "scaffold a new…") | `extension-builder` |

When your intent is ambiguous, the orchestrator asks **one** clarifying question before dispatching. Never a list.

## Language scope

ion-meta helps with **any Ion harness in any language** — TypeScript, Go, Python, Rust, C#, Swift, shell, anything that can read and write JSON-framed lines on stdin/stdout. The Ion engine is a binary speaking JSON-RPC over stdio; the TypeScript and Go SDKs are conveniences over that wire. The contract is the protocol. ion-meta cites `protocol/server-events.md`, `protocol/client-commands.md`, `extensions/sdk-raw.md`, and `extensions/json-rpc-protocol.md` as canonical references *alongside* the SDK docs — not behind them.

## Model tiers

ion-meta agents declare abstract tiers (`fast`, `standard`) in their frontmatter — never concrete model ids. The engine resolves these against your `~/.ion/models.json` `tiers` section; unset tiers fall back to your session default. The orchestrator uses `fast` (intent classification, runs every turn); the specialists use `standard` (cross-doc synthesis, code reading, code generation). The `reasoning` tier is intentionally not preassigned — it stays reserved for explicit user invocation when a question is genuinely architectural.

ion-meta ships no model opinions. Map the tiers to whatever your providers offer:

```jsonc
// ~/.ion/models.json
{
  "tiers": {
    "fast":      { "model": "<your fast model>", "fallbacks": ["<your fallback>"] },
    "standard":  { "model": "<your standard model>" }
  }
}
```

If you do nothing, every agent resolves to your session default. ion-meta still works; it just doesn't optimise across roles. See the engine's models.json docs for the full schema.

## Load it

Either run it directly:

```bash
ion prompt --extension ~/.ion/extensions/ion-meta/index.ts \
  "Scaffold an extension named foo into /tmp/foo with a session_start hook"
```

…or register it as a desktop engine profile so the New Tab → Engine picker (desktop and iOS) exposes it:

> Desktop Settings → **Engine** → **+** (Add) → Name: `ion-meta`, Extensions: `/Users/<you>/.ion/extensions/ion-meta/index.ts`

Restart the desktop after adding a profile by hand-editing `~/.ion/settings.json`; the Settings UI propagates live.

## Specialists

The orchestrator is the conversation itself — the persona injected as the system prompt. The user talks to it directly; it is not a panel row. It is a pure router: it classifies intent and dispatches one of the specialists below. The specialists appear in the desktop Agents panel and flip to `running` when dispatched.

| Agent | Kind | Focus |
|-------|------|-------|
| `ion-tutor` | Mode | Teacher. Explains Ion concepts grounded in canonical docs. Read-only. |
| `extension-improver` | Mode | Pair programmer. Reads an existing harness in any language, proposes and applies targeted improvements. |
| `extension-builder` | Mode | Code generator. Greenfields a new harness end-to-end; verifies via language-appropriate check. |
| `extension-architect` | Knowledge | Extension structure, manifest, build pipeline, JSON-RPC protocol |
| `agent-designer` | Knowledge | Agent `.md` files, hierarchies, parent-child model, discovery |
| `skill-author` | Knowledge | Skill `.md` authoring |
| `hook-specialist` | Knowledge | Every engine hook, payload type, the five return patterns |
| `testing-guide` | Knowledge | Integration tests, MockProvider/MockBackend, JSON-RPC subprocess harness |
| `orchestration-designer` | Knowledge | Dispatch flows, `engine_agent_state` snapshots, the capability surface |

Mode-shaped agents reflect *active intent*; knowledge-shaped agents are deep-dive helpers the orchestrator dispatches when a question is narrow on one surface ("explain `agent_start` payload" → `hook-specialist` rather than `ion-tutor`).

## Tools

All tools are deterministic and source-of-truth-driven. They never paraphrase the SDK or hook list from memory; ion-meta's catalog is parsed from the SDK's live `types.ts`.

| Tool | Purpose |
|------|---------|
| `ion_scaffold` | Generate an extension/agent/skill. With `targetDir` writes files to disk; without, returns templates for preview. |
| `ion_validate_agent` | Validate agent markdown frontmatter (`name`, `description`, `parent`, `model`, `tools` shape) and optionally check the parent reference against sibling agents. |
| `ion_validate_manifest` | Schema-check `extension.json` against the three accepted top-level keys. |
| `ion_list_hooks` | Return the hook catalog from `HookPayloadMap`. Filter by `category` or `name`. Joins with `docs/hooks/reference.md` for payload + use case per hook when the canonical docs are bundled. |
| `ion_list_sdk_methods` | Return the full `IonContext` method list with one-line signatures and descriptions, sourced from `types.ts`. |
| `ion_list_extensions` | Enumerate extensions installed under `~/.ion/extensions/`. |
| `ion_inspect_extension` | Parse an extension's `index.ts` and report registered hooks, tools, commands, imports, and manifest fields. |
| `ion_read_doc` | Read a canonical Ion doc (`extensions/`, `hooks/`, `agents/`, `architecture/`) bundled under `docs/canonical/`. |
| `ion_typecheck_extension` | Run esbuild against an extension and surface parse / import errors. Requires `esbuild` on `PATH`. |

## Hook wiring

ion-meta registers these hooks. They form the "orchestrator spine" the desktop's Agents panel reflects:

| Hook | Behavior |
|------|----------|
| `session_start` | Emit the initial `engine_agent_state` snapshot (every specialist visible, all idle). On a fresh conversation also emit the welcome message — see "Greeting" below. |
| `before_prompt` | Inject the generated persona as `systemPrompt`. Persona is cached per-extensionDir. |
| `tool_call` | Deterministic git-gate. Refuses `Write` / `Edit` / `Bash` / `ion_scaffold` calls targeting paths outside a git working tree. See "Git-gate" below. |
| `agent_start` | Mark the dispatched specialist as `running` in the panel; emit `engine_working_message` |
| `agent_end` | Mark the specialist back to `idle`; preserve the last task as `lastWork` |
| `capability_discover` | Emit telemetry advertising ion-meta's capability set (see "Capability surface caveat" below) |
| `capability_match` | Route free-form user intent to a capability name; emits a custom `ion_meta_intent_routed` event |
| `on_error` | Surface caught errors as `engine_notify` so the desktop user sees them |
| `session_end` | Emit `agents: []` to wipe the panel per the snapshot contract |

### Git-gate

The `tool_call` hook is the canonical example of ion-meta's deterministic-seams philosophy ([ADR-006](../../../docs/architecture/adr/006-deterministic-seams-and-probabilistic-judgment.md)). It refuses every `Write`, `Edit`, `Bash`, or `ion_scaffold` call whose target is outside a git working tree, returning `{ block: true, reason }` to the engine. The LLM cannot override.

Why: reversibility. The improver and builder write to user files; if those files aren't under git, the user has no `git diff` to review and no `git checkout` to back out. By requiring the target be inside a git repo, every edit is auditable and revertible. ion-meta does not maintain its own backup/journal machinery (forbidden per the no-state rule).

Scope and algorithm: see [git-gate.ts](./git-gate.ts) for the detection algorithm (walks up looking for `.git/` directory or `.git` worktree pointer, stops at filesystem root or `~`, caches results). The block reason flowing back to the LLM is verbatim: *"ion-meta refused this … call because `<path>` is not inside a git working tree. … To proceed: (1) move the target into an existing git repo, (2) run `git init` …, or (3) ask me to teach or explain instead of edit."* The gate is unconditional and non-bypassable — no trusted-directories allowlist, no env-var escape hatch.

### Greeting

ion-meta emits a static "Welcome to Ion Meta" markdown as an `engine_harness_message` (source: `ion-meta`) on the first `session_start` of a logically-new conversation. The welcome content lives in `greeting.ts` and is shipped verbatim — no template interpolation, no count placeholders. The welcome introduces the three modes (teach / improve / build) and gives example prompts for each.

Freshness is detected by the absence of any file matching `~/.ion/conversations/<ctx.sessionKey>.*` (see `fresh-session.ts`). The engine writes conversation files only after the first turn is saved, so the first `session_start` of a new conversation observes no file and the welcome fires; the second observes one and the welcome is suppressed. The persona instructs the LLM not to re-greet because the welcome has already rendered above the user's first turn.

The check delegates "have I seen this conversation before" to the engine's persistence layer rather than tracking session keys in harness state, because `ctx.sessionKey` is client-supplied and may be reused. Failure modes are documented in the source.

The welcome is tagged with `metadata.dedupKey: 'ion-meta:welcome'`. The desktop renderer dedups harness messages by `dedupKey` within an engine-instance scrollback, so the welcome appears at most once per tab even if `session_start` fires repeatedly (e.g. app restart with no intervening turn — the filesystem still reads "fresh" because the engine has nothing to persist for a zero-turn conversation). The filesystem-based freshness check is an additional optimization; the renderer is the source of truth for "have I already shown this exact message?". The engine treats `metadata` as opaque pass-through — see `docs/protocol/server-events.md` for the well-known metadata keys reference.

## Capability surface caveat (TS extensions)

The engine wires `capability_discover`, `capability_match`, and `capability_invoke` as string-returning hooks in the TypeScript forwarder, but the Go-side dispatcher expects structured `[]Capability` / `*CapabilityMatchResult` returns. A TS string return is discarded.

This means **TS extensions cannot push capabilities or routing decisions through these hooks directly**. The working TS pattern is **side-effect routing**: call `ctx.registerAgentSpec(...)` from inside the `capability_match` handler and return `undefined`. See `engine/extensions/ion-canary/index.ts` for the canonical example. Or write the extension in Go.

ion-meta uses these hooks for telemetry only; it does not try to feed routing decisions back to the engine through them.

## Architecture

```
engine/extensions/ion-meta/
  index.ts                # createIon() + hook wiring + tool registration + git-gate hook
  catalog.ts              # parses SDK types.ts → live hook + IonContext lists
  persona.ts              # composes the before_prompt persona string
  agent-state.ts          # emit engine_agent_state snapshots (complete-snapshot contract)
  greeting.ts             # canonical first-session welcome markdown
  fresh-session.ts        # detects new vs. continued conversation via persistence files
  git-gate.ts             # deterministic write-gate (refuses tool calls outside a git tree)
  tools/
    scaffold.ts
    validate-agent.ts
    list-hooks.ts
    list-extensions.ts
    inspect-extension.ts
    list-sdk-methods.ts
    read-doc.ts
    validate-manifest.ts
    typecheck-extension.ts
    index.ts              # re-exports
  agents/
    orchestrator.md       # router; model: fast
    ion-tutor.md          # teach mode; read-only
    extension-improver.md # improve mode
    extension-builder.md  # build mode
    extension-architect.md
    agent-designer.md
    skill-author.md
    hook-specialist.md
    testing-guide.md
    orchestration-designer.md
  docs/
    extension-anatomy.md  # pointer to canonical
    hooks-reference.md    # pointer to canonical
    canonical/            # populated by install.command
      extensions/
      hooks/
      agents/
      architecture/
```

## Requirements

- Node.js 18+
- `esbuild` on PATH (`npm i -g esbuild`)
- For the `ion_typecheck_extension` tool: same `esbuild` binary
- For non-TS builder/improver targets: the language's native toolchain on PATH (`python3`, `go`, `cargo`, `dotnet`, etc.)

## Installation

`make engine` (from the repo root, or `make install` inside `engine/`) does:

1. Builds the engine binary.
2. Installs the binary to `~/.ion/bin/ion`.
3. Copies `engine/extensions/sdk/` to `~/.ion/extensions/sdk/`.
4. Copies `engine/extensions/ion-meta/` to `~/.ion/extensions/ion-meta/`.
5. Copies `docs/extensions/`, `docs/hooks/`, `docs/agents/`, and `docs/architecture/` into `~/.ion/extensions/ion-meta/docs/canonical/` so `ion_read_doc` resolves them.

The canonical/ tree is rebuilt on every install (rm -rf + cp -r) so renames and deletions propagate.

## Drift detection

A Claude Code slash command `.claude/commands/ion--update-ion-meta-extension.md` re-runs the same audit that produced this version of ion-meta. Run it whenever the SDK or canonical docs change to spot drift:

```
/ion--update-ion-meta-extension
```

It's read-only; outputs a structured markdown drift report listing missing hooks, unused SDK surfaces, stale counts in human-maintained strings, and tool/persona/doc drift.
