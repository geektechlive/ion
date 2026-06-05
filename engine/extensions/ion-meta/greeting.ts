// ion-meta first-session welcome.
//
// Static markdown emitted as an `engine_harness_message` on `session_start`
// when no on-disk conversation file exists for the session key. See
// `fresh-session.ts` for the detection mechanism and `index.ts` for the
// emission wiring.
//
// The emit attaches `metadata.dedupKey: 'ion-meta:welcome'`. This is the
// renderer-honored pass-through convention: the desktop session store
// suppresses repeated harness messages with the same `dedupKey` within a
// single engine-instance scrollback. The engine treats `metadata` as
// opaque — it forwards the field verbatim and applies no semantics. The
// dedup mechanism is the safety net for the case the filesystem check
// can't catch: app restart with no user turn in between, where the engine
// has nothing to write to disk for a zero-turn conversation so the
// freshness signal stays "fresh." The convention namespace is
// `<extensionName>:<messageKey>`; pick a unique key per message kind.
// See docs/protocol/server-events.md for the well-known-keys table.
//
// Authoring rules (enforced by review, not by code):
//   - No hard-coded counts ("seven specialists", "nine tools"). The
//     orchestrator spine grows over time; the greeting must not drift
//     when a tool or specialist is added. Phrase in suite/family terms.
//   - No references to `.claude/`, slash commands, or other developer-
//     only tooling that ships outside the public ion-meta surface.
//   - **No engine-contributor framing.** ion-meta is a tool for people
//     who *consume* the Ion engine to build their own products and
//     harnesses on top of it. It is NOT a tool for working on the
//     engine itself. Do not surface engine-development discipline
//     ("contracts are additive," "we never rename hooks," "if you ask
//     for an engine change I will push back") — those rules are for
//     the people writing the engine, not the people using it. A
//     consumer cannot rename a hook because they don't write hooks;
//     they consume them. Frame everything from the consumer's seat.
//   - Tone: orient the user, advertise what's reachable, invite a
//     concrete first prompt. Declarative ending — no trailing question,
//     because the welcome sits before any user turn in the transcript.

export const WELCOME_MARKDOWN: string = [
  '# Welcome to Ion Meta',
  '',
  'I am the authoring partner for Ion harnesses — extensions, agents, skills, hooks, and any program that speaks the engine\'s JSON-RPC wire. Everything I tell you about the SDK, hooks, or CLI is grounded in the live engine source. I do not paraphrase from memory.',
  '',
  '## What I can do for you',
  '',
  '- **Teach** — explain Ion\'s wire protocol, the hook catalog, the TypeScript and Go SDKs, the deterministic-seams design principle, and anything else in the canonical docs.',
  '- **Improve** — read your existing harness (any language: TypeScript, Python, Go, Rust, C#, shell — anything that speaks the engine\'s JSON-RPC wire) and propose targeted improvements, especially places where the engine already offers what you re-implemented.',
  '- **Build** — greenfield a new harness end-to-end in whichever language you ask for: finished, verified, and ready to load.',
  '',
  'No mode toggle. Just tell me what you want and I\'ll route to the right specialist.',
  '',
  '## How to ask',
  '',
  'Concrete, specific prompts produce the best results. For example:',
  '',
  '- *"How does `session_start` differ from `before_prompt`?"* (teach)',
  '- *"How would I emit `engine_agent_state` from a Python harness that doesn\'t use the SDK?"* (teach — non-TS)',
  '- *"Look at `~/.ion/extensions/ion-canary` and tell me what could be improved."* (improve)',
  '- *"Audit the scheduler in this Python harness — is it using the right Ion features?"* (improve — non-TS)',
  '- *"Build me a new extension called `inbox-watcher` that emits an `engine_notify` when a file under `~/Mail` changes."* (build)',
  '- *"Scaffold a minimal Python harness that prints every `engine_agent_state` event to stdout."* (build — non-TS)',
  '',
  '## Ground rules',
  '',
  'Everything I tell you about the SDK, hooks, CLI, or wire protocol is grounded in the live engine source and the bundled docs — I do not paraphrase from memory. If you ask about a hook, method, or symbol that does not exist, I will tell you so rather than invent one.',
  '',
  'I write edits only inside a git working tree, so every change is reviewable and revertible via `git diff` / `git checkout`. If you point me at a path outside a repo, I\'ll ask you to `git init` first.',
  '',
  'Tell me what you want to build, or ask me anything about Ion.',
].join('\n')
