// @file-size-exception: test fixture — a 194-line markdown corpus extracted
// verbatim from conversation 1780405865550-54fac261fb2e entry f1c0d1b6.
// Splitting would harm readability of the regression test that proves we
// no longer drop content past the second fenced code block. Treat this as
// an immutable data file: do not edit, comment-out, or reformat.

import Foundation

/// The exact text content of the last assistant message in conversation
/// `1780405865550-54fac261fb2e` (entry `f1c0d1b6`). This corpus reproduces
/// the iOS truncation defect: the Foundation `AttributedString(markdown:)`
/// parser silently drops everything after the second fenced code block in
/// Step 10. swift-markdown parses it in full. The regression test asserts
/// that the closing `STOP — awaiting confirmation` line is reachable.
enum MarkdownFormatterFixtures {
    static let longMixedContent: String = #"""
**File overlap confirmed.** Every engine fix touches files introduced or heavily modified by `f7e202f`:

| Commit | Files | Overlap with f7e202f |
|--------|-------|---------------------|
| 268d708 | `providers/compact_boundary_test.go` (new), `conversation/compact_boundary_persistence_test.go` (new) | Tests for `providers/anthropic.go`, `providers/openai.go`, `conversation/compact_boundary.go` (all f7e202f) |
| e96e93e | `backend/backend.go`, `runloop_compact_boundary_test.go`, `runloop_compaction.go`, `session/prompt_runconfig.go` | All four files modified in f7e202f |
| f1f8293 | `runloop_compaction.go`, `compaction/compaction.go`, `compaction/compact_boundary_test.go` | All three modified in f7e202f |
| a3518ec | `backend/runloop_compact_boundary_test.go` | Created in f7e202f |
| b4109b7 | `extension/sdk_hooks_compact_summary_test.go` (new) | Tests `extension/sdk_hooks_session.go` and `sdk.go` (both f7e202f) |
| 5f74d0d | `docs/hooks/reference.md`, `docs/extensions/sdk-*.md`, `engine/AGENTS.md` | Documents the hook added in f7e202f |

**Temporal proximity:** Commits 1–4 are within ~2 hours on 2026-05-28. Commits 5–9 are within ~8 minutes on 2026-06-02. Two work sessions, both extending the same f7e202f foundation.

**Greenfield signal:** f7e202f introduces `compact_boundary.go` (139 new lines), `compact_boundary_test.go`, the `LlmContentBlock` `compact_boundary` variant, the `OnRequestCompactSummary` hook, the `HookCompactSummaryRequest` SDK hook, `BuildCompactBoundaryMessage`, `MessagesAfterLastCompactBoundary`, `PostCompactReset`, `renderCompactSummary`, four-tier ladder. Every follower extends or pins this foundation. Strong greenfield signal despite the `fix(engine)` label.

## Step 5: Trace Fix → Feature Relationships

The chain root (`f7e202f`) is labeled `fix` but is actually greenfield. Per Step 4's chain rule, the chain root keeps its position. The chain type promotion goes `feat!` > `feat` > `fix` > `chore`. Since there are no `feat(engine)` commits in this chain, the synthesized type is `fix(engine)`.

Let me trace each follower:

- **268d708** (fix, engine): "pin compact_boundary wire and persistence contracts" — body literally says "gentle-knitting-cup follow-up audit" referring to f7e202f. **Chain follower.**
- **e96e93e** (fix, engine): explicitly references "added as a 2-arg function ... in commit f7e202f" and threads through Strategy that f7e202f introduced. **Chain follower.**
- **f1f8293** (chore, engine): "renderCompactSummary was added in commit f7e202f as the documented single rendering decision point ... The helper was dead code." Completes the extraction f7e202f started. **Chain follower (cleanup).**
- **a3518ec** (fix, engine): "compactReactive had four structural assertion tests in runloop_compact_boundary_test.go [added by f7e202f], but compactIfNeeded had only the pre-existing tests." Adds proactive-path coverage. **Chain follower.**
- **b4109b7** (fix, engine): covers `FireCompactSummaryRequest` (introduced by f7e202f) return shapes. **Chain follower.**
- **5f74d0d** (docs, engine): documents the `compact_summary_request` hook added by f7e202f. **Chain follower (docs).**

## Step 6: Repeated Fix Clusters

No repeated fix clusters in the strict sense (no two commits attack the same problem with similar messages). Each follower targets a distinct gap: provider wire test, persistence test, strategy threading, helper extraction, proactive-path test, return-shape test, docs. These are sequential gap-closures on the same foundation, which is exactly what a chain looks like.

## Step 7: Superseding Rewrites

None. The chain only extends and refines f7e202f's foundation — it doesn't replace anything.

## Cross-scope check: 7580242 and 0d1994a

- **7580242** `feat(desktop)`: mirrors `LlmContentBlock` into `desktop/src/shared/types-engine.ts`. Same logical feature as f7e202f but different scope.
- **0d1994a** `docs(ios)`: notes that iOS intentionally does not mirror `LlmContentBlock`. Same logical feature, different scope.

Per Step 4d, **cross-scope pairs are NOT squashed**. These stay as separate commits. They are informationally related to the engine chain but live in different scopes per the repo's commit-scope rules in `CLAUDE.md`.

Also, both `7580242` and `0d1994a` are independent of each other only in scope — they document the same engine change for two different clients. They each stand alone in their respective scopes.

## Step 8: Classify Every Commit

| # | Hash | Disposition | Rebase action |
|---|------|-------------|---------------|
| 1 | f7e202f | **Chain root** (engine compaction) | `pick` |
| 2 | 7580242 | Standalone (cross-scope: desktop) | `pick` |
| 3 | 0d1994a | Standalone (cross-scope: ios) | `pick` |
| 4 | 268d708 | Chain follower (engine) | `fixup` after f7e202f |
| 5 | e96e93e | Chain follower (engine) | `fixup` after f7e202f |
| 6 | f1f8293 | Chain follower (engine, chore) | `fixup` after f7e202f |
| 7 | a3518ec | Chain follower (engine) | `fixup` after f7e202f |
| 8 | b4109b7 | Chain follower (engine) | `fixup` after f7e202f |
| 9 | 5f74d0d | Chain follower (engine, docs) | `fixup` after f7e202f |

## Conflict risk for moved followers

Followers 4–9 must move past commits 2 (desktop) and 3 (ios) to land adjacent to f7e202f. Both intervening commits touch **only** `desktop/` and `ios/` files. All chain followers touch **only** `engine/` or `docs/` files. **Zero file overlap — zero conflict risk.**

## Step 9: Synthesize Chain Commit Message

The root's subject "structurally dedupe compaction summaries" undersells the actual scope (it introduces `compact_boundary`, a new SDK hook, and a four-tier rendering ladder). Followers broaden it meaningfully (strategy threading, contract pinning, hook docs). The synthesized subject should reflect the full theme.

Proposed synthesized message:

```
fix(engine): introduce compact_boundary and dedupe summaries

- structurally dedupe compaction summaries
- pin compact_boundary wire and persistence contracts
- thread compact strategy through OnRequestCompactSummary
- route compact summary through one helper
- add proactive-path compact_boundary injection test
- cover FireCompactSummaryRequest fan-out return shapes
- document compact_summary_request hook
```

Subject is 49 chars (≤ 50 ✓), lowercase, imperative, no period, `fix(engine):` scope matches the root and the highest-priority type in the chain (no `feat` in the chain). Body lists each absorbed commit's original description, oldest to newest.

## Step 10: Rebase Plan

```
pick   f7e202f fix(engine): structurally dedupe compaction summaries          # CHAIN ROOT (will reword)
fixup  268d708 fix(engine): pin compact_boundary wire and persistence contracts
fixup  e96e93e fix(engine): thread compact strategy through OnRequestCompactSummary
fixup  f1f8293 chore(engine): route compact summary through one helper
fixup  a3518ec fix(engine): add proactive-path compact_boundary injection test
fixup  b4109b7 fix(engine): cover FireCompactSummaryRequest fan-out return shapes
fixup  5f74d0d docs(engine): document compact_summary_request hook
pick   7580242 feat(desktop): mirror LlmContentBlock with compact_boundary fields
pick   0d1994a docs(ios): note LlmContentBlock is intentionally not mirrored
```

Approach: I'll do this in two passes.
1. First pass: non-interactive rebase with a scripted todo that moves all chain followers adjacent to the root and marks them `fixup`. Use `GIT_SEQUENCE_EDITOR` to write the todo deterministically.
2. After fixup completes, amend the (now-combined) chain root commit with the synthesized message via `git commit --amend -F <file>`.

This avoids running `git rebase -i` twice and removes the `reword` complication.

## Step 11: Analysis Presentation

### Backup status
`backup--work` already existed at HEAD `5f74d0d`. ✓

### Commit summary
- Total unpushed: **9 commits**
- By type: 5 `fix`, 1 `feat`, 1 `chore`, 2 `docs`
- By scope: 7 `engine`, 1 `desktop`, 1 `ios`

### Feature chains (the one chain found)

**Chain: engine compaction / compact_boundary**

| Role | Hash | Original subject |
|------|------|------------------|
| Root | f7e202f | fix(engine): structurally dedupe compaction summaries |
| Follower | 268d708 | fix(engine): pin compact_boundary wire and persistence contracts |
| Follower | e96e93e | fix(engine): thread compact strategy through OnRequestCompactSummary |
| Follower | f1f8293 | chore(engine): route compact summary through one helper |
| Follower | a3518ec | fix(engine): add proactive-path compact_boundary injection test |
| Follower | b4109b7 | fix(engine): cover FireCompactSummaryRequest fan-out return shapes |
| Follower | 5f74d0d | docs(engine): document compact_summary_request hook |

**Rationale:**
- **Subject overlap:** every follower references `compact_boundary`, `compact_summary`, `OnRequestCompactSummary`, or `FireCompactSummaryRequest` — all introduced by f7e202f.
- **Explicit hash citations:** four followers (268d708, e96e93e, f1f8293, 5f74d0d) name f7e202f by hash in their bodies.
- **File overlap:** every follower touches files created or modified by f7e202f (`runloop_compaction.go`, `runloop_compact_boundary_test.go`, `compaction.go`, `backend.go`, `prompt_runconfig.go`, `compact_boundary.go`, providers, SDK hooks).
- **Temporal proximity:** two work sessions (2026-05-28 ~2h window, 2026-06-02 ~8min window) all on the same subsystem.
- **Greenfield signal:** f7e202f introduces 1316 lines of new infrastructure (new files `compact_boundary.go`, new SDK hook, new content-block variant, new helpers). Followers test, refine, and document this foundation — they do not modify pre-existing behavior.

**Proposed synthesized commit message:**

```
fix(engine): introduce compact_boundary and dedupe summaries

- structurally dedupe compaction summaries
- pin compact_boundary wire and persistence contracts
- thread compact strategy through OnRequestCompactSummary
- route compact summary through one helper
- add proactive-path compact_boundary injection test
- cover FireCompactSummaryRequest fan-out return shapes
- document compact_summary_request hook
```

### Fix follow-ups (non-chain)
None. All fixes belong to the chain.

### Repeated fix clusters
None. Each follower closes a distinct gap.

### Superseding rewrites
None.

### Cross-scope pairs (informational only — NOT squashed)
- **7580242** `feat(desktop)`: mirrors the engine's new `LlmContentBlock.compact_boundary` fields into TypeScript for contract sync.
- **0d1994a** `docs(ios)`: documents that iOS intentionally does not mirror `LlmContentBlock`.

Both relate to f7e202f's engine work but belong to other scopes per `.commit.json` rules. They stay as separate commits.

### Standalone commits
- **7580242** `feat(desktop)` — cross-scope, kept as-is.
- **0d1994a** `docs(ios)` — cross-scope, kept as-is.

### Before / after count
**9 commits → 3 commits (6 squashed away)**

### Full rebase plan (annotated)

```
# Group: engine compaction chain
pick   f7e202f fix(engine): structurally dedupe compaction summaries          # ROOT (will be amended with synthesized message)
fixup  268d708 fix(engine): pin compact_boundary wire and persistence contracts
fixup  e96e93e fix(engine): thread compact strategy through OnRequestCompactSummary
fixup  f1f8293 chore(engine): route compact summary through one helper
fixup  a3518ec fix(engine): add proactive-path compact_boundary injection test
fixup  b4109b7 fix(engine): cover FireCompactSummaryRequest fan-out return shapes
fixup  5f74d0d docs(engine): document compact_summary_request hook

# Group: cross-scope (kept separate)
pick   7580242 feat(desktop): mirror LlmContentBlock with compact_boundary fields
pick   0d1994a docs(ios): note LlmContentBlock is intentionally not mirrored
```

**Conflict risk:** Low/zero. The chain followers (engine + docs files) are being moved past the desktop and ios commits, but the file sets are disjoint. No textual conflicts expected.

After the rebase, `git commit --amend -F -` is used on the combined chain root to apply the synthesized message.

---

⚠️ **STOP — awaiting confirmation.**
"""#
}
