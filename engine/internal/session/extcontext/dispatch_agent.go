package extcontext

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/session/agents"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// ExitCodeRecalled is the exit code used when a dispatch is cancelled via
// RecallAgent. Distinct from 0 (success) and 1 (error) so consumers can
// distinguish recall from failure.
const ExitCodeRecalled = 2

// DefaultMaxDispatchDepth is the built-in cap when neither the per-dispatch
// override (DispatchAgentOpts.MaxDispatchDepth) nor the engine config
// (EngineRuntimeConfig.MaxDispatchDepth) sets a value. Allows depths
// 0 (orchestrator), 1, and 2.
const DefaultMaxDispatchDepth = 3

// ErrDispatchDepthExceeded is returned by DispatchAgent when the requested
// dispatch would exceed the effective MaxDispatchDepth. The caller sees a
// typed error so it can distinguish depth rejection from other failures.
var ErrDispatchDepthExceeded = errors.New("dispatch depth exceeded")

// ErrSelfDispatch and ErrSubAgentNotAllowed (the eligibility-guard errors)
// are defined in dispatch_eligibility.go alongside the guard that returns them.

// resolveMaxDispatchDepth returns the effective depth cap for a dispatch,
// preferring the per-dispatch override, then the engine config, then the
// built-in default.
func resolveMaxDispatchDepth(perDispatch int, engineCfg int) int {
	if perDispatch > 0 {
		return perDispatch
	}
	if engineCfg > 0 {
		return engineCfg
	}
	return DefaultMaxDispatchDepth
}

// BuildDispatchAgentFunc returns the DispatchAgent closure. currentDepth is
// the owning agent's depth (0=orchestrator). currentDispatchId is the owning
// agent's dispatch ID (empty at depth 0). The child inherits depth+1.
//
// Background dispatch returns a stub immediately and runs in a goroutine;
// terminal outcome via OnComplete/OnError/OnRecall callbacks.
// Phase 2 lifecycle callbacks fire from OnNormalized; Phase 3 telemetry
// (engine_dispatch_start/end) emit on the parent session's event stream.
func BuildDispatchAgentFunc(sa SessionAccessor, registry *DispatchRegistry, currentDepth int, currentDispatchId string) func(extension.DispatchAgentOpts) (*extension.DispatchAgentResult, error) {
	return func(opts extension.DispatchAgentOpts) (*extension.DispatchAgentResult, error) {
		// --- Depth guard ---
		childDepth := currentDepth + 1
		var engineMaxDepth int
		if cfg := sa.EngineConfig(); cfg != nil {
			engineMaxDepth = cfg.MaxDispatchDepth
		}
		effectiveCap := resolveMaxDispatchDepth(opts.MaxDispatchDepth, engineMaxDepth)

		if childDepth >= effectiveCap {
			utils.Warn("Dispatch", fmt.Sprintf(
				"depth guard: blocked dispatch agent=%q childDepth=%d cap=%d parentDispatchId=%q session=%s",
				opts.Name, childDepth, effectiveCap, currentDispatchId, sa.SessionKey(),
			))
			return nil, fmt.Errorf("%w: agent=%q would be depth %d (cap %d)", ErrDispatchDepthExceeded, opts.Name, childDepth, effectiveCap)
		}

		utils.Log("Dispatch", fmt.Sprintf(
			"depth guard: allowed dispatch agent=%q childDepth=%d cap=%d parentDispatchId=%q session=%s",
			opts.Name, childDepth, effectiveCap, currentDispatchId, sa.SessionKey(),
		))

		// --- Eligibility guard ---
		// Enforce the self-dispatch rail (an agent may not dispatch its own
		// name) and the DISPATCHER's carry-forward AllowedSubAgents allowlist
		// (resolved from currentDispatchId in the registry). Skipped at depth 0
		// (the orchestrator has no dispatcher entry). Logic lives in
		// dispatch_eligibility.go to keep this file under the 800-line cap.
		if err := checkDispatchEligibility(sa, registry, currentDispatchId, opts.Name); err != nil {
			return nil, err
		}

		start := time.Now()

		utils.Log("Dispatch", fmt.Sprintf(
			"starting dispatch agent=%q task=%q model=%q sysPromptLen=%d background=%v planMode=%v session=%s",
			opts.Name, truncate(opts.Task, 80), opts.Model, len(opts.SystemPrompt), opts.Background, opts.PlanMode, sa.SessionKey(),
		))

		// Determine model and project path.
		model := opts.Model
		if model == "" {
			if cfg := sa.EngineConfig(); cfg != nil {
				model = cfg.DefaultModel
			}
		}
		projectPath := opts.ProjectPath
		projectPathSource := "opts" // logged below; both branches observable
		if projectPath == "" {
			projectPath = sa.WorkingDirectory()
			projectPathSource = "fallback"
		}

		// --- Agent state management ---
		// Create or update an agent state entry in the parent session's
		// registry so the agent panel shows the dispatch. This mirrors
		// what prompt_agent_spawner does for LLM-initiated Agent tool calls.
		agentID := fmt.Sprintf("dispatch-%s-%d-%s", opts.Name, start.UnixMilli(), conversation.NewConvSuffix())
		agentName := opts.Name
		key := sa.SessionKey()
		logDispatchWorkdir(agentName, projectPath, projectPathSource, agentID, childDepth, key)

		// Look up the spec to get a display name and tool restrictions.
		displayName := agentName
		var specTools []string
		if spec, ok := sa.LookupAgentSpec(agentName); ok {
			if spec.Description != "" {
				displayName = spec.Description
			}
			specTools = spec.Tools
		}
		// Fallback: inherit the display name from the extension's cached roster.
		// Extensions provide displayName via roster metadata, not via AgentSpec.
		if displayName == agentName {
			if dn := sa.LookupExtDisplayName(agentName); dn != "" {
				displayName = dn
			}
		}
		// Caller override: when the dispatcher supplied an explicit display
		// name (e.g. the orchestrator's Agent tool passes the call-site
		// description), honor it over the spec/roster resolution above.
		if opts.DisplayName != "" {
			displayName = opts.DisplayName
		}

		newDispatch := map[string]interface{}{
			"id":        agentID,
			"task":      opts.Task,
			"model":     model,
			"status":    "running",
			"startTime": start.Unix(),
		}
		sa.AppendOrUpdateAgentState(types.AgentStateUpdate{
			Name:   agentName,
			ID:     agentID,
			Status: "running",
			Metadata: map[string]interface{}{
				"displayName": displayName,
				"type":        "agent",
				"visibility":  "sticky",
				"invited":     true,
				"task":        opts.Task,
				"model":       model,
				"startTime":   start.Unix(),
				"dispatches":  []interface{}{newDispatch},
				// Nesting attribution so consumers can isolate nested
				// dispatches from root-level ones. childDepth is this agent's
				// depth (1=direct child of orchestrator, 2=grandchild, ...);
				// currentDispatchId is the parent dispatch's id (empty when the
				// orchestrator dispatched directly). The desktop/iOS main panels
				// filter to root-level agents (depth<=1) so a lead's specialists
				// appear only inside the lead's dispatch preview, not the main
				// conversation row. Mirrors the dispatchDepth/dispatchParentId
				// already carried on engine_dispatch_start telemetry below.
				"dispatchDepth":    childDepth,
				"dispatchParentId": currentDispatchId,
			},
		})
		sa.EmitAgentSnapshot("dispatch_start")

		// Fire agent_start on the parent extension group so the extension's
		// roster row flips to running.
		if extGroup := sa.ExtGroup(); extGroup != nil && !extGroup.IsEmpty() {
			utils.Log("Dispatch", fmt.Sprintf("firing agent_start key=%s name=%s id=%s", key, agentName, agentID))
			startCtx := NewExtContext(sa)
			extGroup.FireAgentStart(startCtx, extension.AgentInfo{
				Name: agentName,
				Task: opts.Task,
			})
		}

		// --- Live progress forwarding ---
		var (
			progressMu   sync.Mutex
			textAccum    string
			lastEmitTime time.Time
		)
		const progressInterval = 2 * time.Second
		const maxSnippetLen = 100

		emitProgress := func(work string) {
			if len(work) > maxSnippetLen {
				work = work[:maxSnippetLen]
			}
			sa.UpdateAgentStateByID(agentID, func(state *types.AgentStateUpdate) {
				if state.Metadata == nil {
					state.Metadata = map[string]interface{}{}
				}
				state.Metadata["lastWork"] = work
			})
			sa.EmitAgentSnapshot("dispatch_progress")
		}

		// Live intra-turn transcript forwarding. The emitter pushes the child's
		// tool calls, tool results, and streamed text to the parent session's
		// client stream as engine_dispatch_activity events so consumers can
		// present the live sub-agent transcript without waiting for completion.
		// Closed in runChild once the dispatch finishes (flushes trailing text).
		activity := NewDispatchActivityEmitter(sa.Emit, agentID, agentName)

		// Create child backend matching the parent session's backend type.
		child := sa.NewChildBackend()
		var childCfg *backend.RunConfig

		childExtHost := loadChildExtension(sa, registry, &opts, model, projectPath, childDepth, agentID)
		if childExtHost != nil {
			childCfg = &backend.RunConfig{
				Hooks: backend.RunHooks{
					OnToolCall: func(info backend.ToolCallInfo) (*backend.ToolCallResult, error) {
						tcCtx := NewExtContext(sa, ExtContextOpts{
							Depth:      childDepth,
							DispatchId: agentID,
							Registry:   registry,
						})
						result, _ := childExtHost.FireToolCall(tcCtx, extension.ToolCallInfo{
							ToolName: info.ToolName,
							ToolID:   info.ToolID,
							Input:    info.Input,
						})
						if result != nil && result.Block {
							return &backend.ToolCallResult{Block: true, Reason: result.Reason}, nil
						}
						return nil, nil
					},
				},
			}
		}

		// Thread DefaultModel so the runloop fallback fires when the child's
		// model doesn't resolve. Mirrors prompt_agent_spawner.go.
		var dispatchDefaultModel string
		if engCfg := sa.EngineConfig(); engCfg != nil {
			dispatchDefaultModel = engCfg.DefaultModel
		}
		if childCfg == nil {
			childCfg = &backend.RunConfig{DefaultModel: dispatchDefaultModel}
		} else if childCfg.DefaultModel == "" {
			childCfg.DefaultModel = dispatchDefaultModel
		}
		utils.Log("Session", fmt.Sprintf("child run config: defaultModelThreaded=%q source=dispatch sessionKey=%s requestedModel=%q", dispatchDefaultModel, sa.SessionKey(), model))

		// Wire AgentSpawner so the child can dispatch grandchildren via the
		// engine Agent tool (see dispatch_child_spawner.go for rationale).
		childCfg.AgentSpawner = BuildChildAgentSpawner(sa, registry, childDepth, agentID)

		// Wire ChildElicitFn so a dispatched child's AskUserQuestion blocks
		// and surfaces to the dispatcher via OnChildQuestion instead of
		// terminating the child run. When OnChildQuestion is nil the field is
		// left unset and the runloop falls through to the standard
		// terminate-the-run path.
		if opts.OnChildQuestion != nil {
			if childCfg == nil {
				childCfg = &backend.RunConfig{}
			}
			childCfg.ChildElicitFn = buildChildElicitFn(opts.OnChildQuestion, opts.Name, agentID, childDepth)
		}

		// Shared mutable state for the event handler closure.
		var totalCost float64
		var totalInputTokens, totalOutputTokens int
		var totalCacheReadTokens, totalCacheCreationTokens int
		var childSessionID string
		var resultText string
		var childErr error
		var childDone sync.WaitGroup
		childDone.Add(1)

		// Estimated reasoning-token total for the child run (issue #158),
		// accumulated from the child's ThinkingBlockEndEvent stream. Surfaced
		// on DispatchAgentResult.ThinkingTokens / engine_dispatch_end so cost
		// and audit consumers can separate reasoning spend from user-facing
		// output. Estimate caveat: see ThinkingBlockEndEvent.TotalTokens.
		var totalThinkingTokens int

		// Phase 2: Lifecycle callback accumulators.
		var toolCount int
		var accumulatedText string
		// Per-turn cumulative usage tracking (only grows).
		var cumulativeInputTokens, cumulativeOutputTokens int
		var cumulativeCost float64
		// Track active tool names by ID for structured callbacks.
		toolNames := make(map[string]string)
		// lifecycleMu guards the Phase 2 lifecycle accumulators above
		// (toolNames, toolCount, accumulatedText, and the cumulative
		// usage/cost counters). The child's OnNormalized callback is invoked
		// concurrently: tool results are emitted from inside the parallel tool
		// errgroup (backend.executeTools runs each tool in its own goroutine,
		// and each goroutine routes its events through the same callback), so
		// when a child runs N tools in parallel, N goroutines enter the
		// callback at once. Without this lock the unsynchronized map writes in
		// fireLifecycleCallbacks trip Go's "concurrent map writes" fatal, which
		// bypasses recover() and hard-kills the engine process. Mirrors the
		// progressMu pattern below, which already guards the live-progress
		// accumulators in the same callback.
		var lifecycleMu sync.Mutex

		// Plan mode tracking.
		var childPlanFilePath string
		var childPlanExited bool

		// Cancellation context for background dispatch / recall support.
		// Derived from the session cancellation root (sa.RootContext())
		// rather than context.Background() so a session-level abort
		// cancels this dispatch's context alongside its explicit recall
		// path. The child agent typically runs as a separate process, so
		// the authoritative kill is still the OS-process reap in the
		// session manager's abortAllDescendants (killProcess by PID) — this
		// context cancel is the in-process half (it unblocks any
		// goroutine selecting on ctx.Done() here, e.g. background recall
		// wiring), keeping dispatch consistent with the unified tree.
		//
		// Parent selection: when the caller supplied opts.ParentCtx (the
		// orchestrator's Agent tool passes its per-tool-call context), derive
		// from it so cancelling that call cancels this dispatch. The tool-call
		// context is itself derived from the session, so a session abort still
		// cascades. When nil, fall back to the session cancellation root --
		// the prior behavior for extension-initiated dispatches.
		dispatchParentCtx := sa.RootContext()
		if opts.ParentCtx != nil {
			dispatchParentCtx = opts.ParentCtx
		}
		ctx, cancelFn := context.WithCancel(dispatchParentCtx)
		var recalled bool
		var recallReason string

		child.OnNormalized(func(_ string, ev types.NormalizedEvent) {
			// Report child liveness to the parent run's progress watchdog.
			// A genuine child event proves the dispatch is alive; the parent
			// run is parked in the deadline-exempt Agent tool call and emits
			// no progress of its own, so without this it could be flagged as
			// stalled once the self-emitted ToolStalledEvent advisory stopped
			// counting as progress. See sessionAccessor.BumpParentProgress.
			sa.BumpParentProgress()

			ee := sa.TranslateEvent(ev, 0)
			if ee.Type != "" {
				if opts.OnEvent != nil {
					opts.OnEvent(ee)
				}
			}

			// Phase 2: Structured lifecycle callbacks. Guarded by lifecycleMu
			// because this callback runs concurrently across the parallel tool
			// errgroup (see lifecycleMu declaration); fireLifecycleCallbacks
			// mutates the shared accumulator map and scalars.
			lifecycleMu.Lock()
			fireLifecycleCallbacks(&opts, ev, agentID, toolNames, &toolCount, &accumulatedText,
				&cumulativeInputTokens, &cumulativeOutputTokens, &cumulativeCost)
			lifecycleMu.Unlock()

			// Live progress forwarding for the agent panel.
			switch e := ev.Data.(type) {
			case *types.SessionInitEvent:
				// Capture the child's conversation ID the moment the child run
				// initializes — well before TaskCompleteEvent fires at the end.
				// The child emits SessionInitEvent early (runloop.go) and then
				// persists its conversation incrementally, so surfacing the id
				// now lets clients read and stream the live transcript while the
				// dispatch is still running instead of only after it completes.
				//
				// Fire exactly once: SessionInitEvent is emitted per child run,
				// and the terminal runChild update (below) overwrites the same
				// id idempotently with the final status/elapsed.
				if e.SessionID != "" && childSessionID == "" {
					childSessionID = e.SessionID
					// Tell the activity emitter the child conversation id so its
					// pushed deltas carry the reconcile key.
					activity.SetConversationID(childSessionID)
					if registry != nil {
						registry.SetChildConvID(agentID, childSessionID)
					}
					recordChildConvID(sa, agentID, childSessionID, opts.Name, start)
				}
			case *types.TextChunkEvent:
				// Push the streamed text to the live transcript (coalesced).
				activity.AccumulateText(e.Text)
				progressMu.Lock()
				textAccum += e.Text
				now := time.Now()
				shouldEmit := now.Sub(lastEmitTime) >= progressInterval
				snippet := textAccum
				if shouldEmit {
					lastEmitTime = now
					if len(snippet) > maxSnippetLen {
						snippet = snippet[len(snippet)-maxSnippetLen:]
					}
				}
				progressMu.Unlock()
				if shouldEmit {
					emitProgress(snippet)
				}
			case *types.ToolCallEvent:
				// Push the tool-call start to the live transcript.
				activity.HandleToolStart(e.ToolName, e.ToolID)
				progressMu.Lock()
				lastEmitTime = time.Now()
				textAccum = ""
				progressMu.Unlock()
				emitProgress(fmt.Sprintf("Using %s...", e.ToolName))
			case *types.ToolResultEvent:
				// Push the tool-result completion to the live transcript
				// (status-only; reconcile carries the full result body).
				activity.HandleToolEnd(e.ToolID, e.IsError)
			}

			// Track plan mode state from child events.
			switch pe := ev.Data.(type) {
			case *types.ThinkingBlockEndEvent:
				// Accumulate the child's estimated reasoning tokens. Redacted
				// blocks carry 0 (no readable text), so this naturally counts
				// only readable reasoning.
				totalThinkingTokens += pe.TotalTokens
			case *types.PlanModeChangedEvent:
				if pe.PlanFilePath != "" {
					childPlanFilePath = pe.PlanFilePath
					utils.Debug("Dispatch", fmt.Sprintf(
						"child plan file path updated agent=%q planFilePath=%q session=%s",
						opts.Name, childPlanFilePath, sa.SessionKey(),
					))
				}
			case *types.PlanProposalEvent:
				childPlanExited = true
				if pe.PlanFilePath != "" {
					childPlanFilePath = pe.PlanFilePath
				}
				utils.Debug("Dispatch", fmt.Sprintf(
					"child plan exited agent=%q planFilePath=%q session=%s",
					opts.Name, childPlanFilePath, sa.SessionKey(),
				))
			}

			// Capture final result, cost, and session ID from TaskCompleteEvent.
			if tc, ok := ev.Data.(*types.TaskCompleteEvent); ok {
				resultText = tc.Result
				totalCost = tc.CostUsd
				if tc.Usage.InputTokens != nil {
					totalInputTokens = *tc.Usage.InputTokens
				}
				if tc.Usage.OutputTokens != nil {
					totalOutputTokens = *tc.Usage.OutputTokens
				}
				if tc.Usage.CacheReadInputTokens != nil {
					totalCacheReadTokens = *tc.Usage.CacheReadInputTokens
				}
				if tc.Usage.CacheCreationInputTokens != nil {
					totalCacheCreationTokens = *tc.Usage.CacheCreationInputTokens
				}
				if tc.SessionID != "" {
					childSessionID = tc.SessionID
				}
			}
		})
		child.OnExit(func(_ string, _ *int, _ *string, _ string) {
			childDone.Done()
		})
		child.OnError(func(_ string, err error) {
			childErr = err
		})

		// Assemble the child run options. Extracted to buildDispatchRunOptions
		// (dispatch_runopts.go) to keep this file under the 800-line cap.
		runOpts := buildDispatchRunOptions(&opts, model, projectPath, dispatchParentCtx)
		// When the caller supplied no explicit allowlist, scope the child to
		// the matched agent spec's declared tools -- the spec `tools:`
		// frontmatter is the child allowlist contract for staff dispatch.
		if len(runOpts.AllowedTools) == 0 && len(specTools) > 0 {
			runOpts.AllowedTools = specTools
		}

		// Wire a per-dispatch ToolServer for the child CLI subprocess (issue #981).
		// Without this, McpConfig stays empty and harness-registered tools are
		// unresolvable. Root sessions use session.Manager.wireToolServer instead.
		var childToolServer *backend.ToolServer
		if childExtHost != nil {
			childToolServer = BuildChildToolServer(
				child, childExtHost.Tools(), sa, agentID, childDepth, registry, &runOpts,
			)
		}

		key = sa.SessionKey()
		// The child run id must be unique per dispatch INSTANCE. Derive it from
		// agentID, which already carries a per-dispatch uniqueness suffix
		// (dispatch-<name>-<millis>-<NewConvSuffix()>, built above). Deriving it
		// from name + UnixMilli() alone is NOT unique: two dispatches of the same
		// agent name that start in the same millisecond collide on the run id,
		// the child backend reuses one conversation for both, and one dispatch
		// entry is left without its own conversationId. agentID's NewConvSuffix()
		// guarantees distinctness even for same-millisecond concurrent dispatches.
		childReqID := fmt.Sprintf("%s-%s", key, agentID)

		// Phase 3: Emit dispatch_start telemetry on the parent session.
		sa.Emit(types.EngineEvent{
			Type:              "engine_dispatch_start",
			DispatchAgent:     opts.Name,
			DispatchTask:      opts.Task,
			DispatchModel:     model,
			DispatchSessionID: childReqID,
			DispatchDepth:     childDepth,
			DispatchParentId:  currentDispatchId,
			DispatchId:        agentID,
		})

		// runChild encapsulates the child backend start + wait + result
		// building logic. It is called directly for foreground dispatches
		// and in a goroutine for background dispatches.
		runChild := func() *extension.DispatchAgentResult {
			startChild(child, childReqID, runOpts, childCfg)

			// Wait for the child to finish, but also watch for context
			// cancellation (recall).
			doneCh := make(chan struct{})
			go func() {
				childDone.Wait()
				close(doneCh)
			}()

			select {
			case <-doneCh:
				// Normal completion.
			case <-ctx.Done():
				// Recall: cancel the child backend and wait for it to drain.
				utils.Log("Dispatch", fmt.Sprintf(
					"recall context cancelled agent=%q reason=%q session=%s",
					opts.Name, recallReason, key,
				))
				child.Cancel(childReqID)
				<-doneCh
				recalled = true
			}

			elapsed := time.Since(start).Seconds()

			// Flush any trailing buffered transcript text and stop the
			// activity emitter's coalesce timer now that the child is done.
			activity.Close()

			// Cleanup child extension.
			if childExtHost != nil {
				childExtHost.Dispose()
			}
			// Stop the child ToolServer after the subprocess exits (issue #981).
			if childToolServer != nil {
				childToolServer.Stop()
			}

			// Deregister from the dispatch registry (both foreground and background).
			if registry != nil {
				registry.Deregister(agentID)
				// Re-emit engine_status with the updated BackgroundAgents count so
				// the parent session clears its "waiting on background agent" state.
				// handleRunExit sampled bgCount BEFORE Deregister ran; nothing
				// re-emits after, leaving a stale BackgroundAgents:1 (or N) as the
				// last value the client sees. This call is the correction.
				sa.EmitDispatchCountStatus("dispatch_deregister")
			}

			// Build the result.
			exitCode := 0
			output := resultText
			if recalled {
				exitCode = ExitCodeRecalled
				output = fmt.Sprintf("recalled: %s", recallReason)
			} else if childErr != nil {
				exitCode = 1
				output = childErr.Error()
			}

			result := &extension.DispatchAgentResult{
				DispatchID:               agentID,
				Output:                   output,
				ExitCode:                 exitCode,
				Elapsed:                  elapsed,
				Cost:                     totalCost,
				InputTokens:              totalInputTokens,
				OutputTokens:             totalOutputTokens,
				ThinkingTokens:           totalThinkingTokens,
				CacheReadInputTokens:     totalCacheReadTokens,
				CacheCreationInputTokens: totalCacheCreationTokens,
				SessionID:                childSessionID,
				PlanFilePath:             childPlanFilePath,
				PlanExited:               childPlanExited,
				Depth:                    childDepth,
				ParentDispatchId:         currentDispatchId,
			}

			// Update agent state with terminal status and conversation ID.
			sa.UpdateAgentStateByID(agentID, func(state *types.AgentStateUpdate) {
				if state.Metadata == nil {
					state.Metadata = map[string]interface{}{}
				}
				if recalled {
					state.Status = "cancelled"
					state.Metadata["lastWork"] = "cancelled: " + recallReason
				} else if childErr != nil {
					state.Status = "error"
					state.Metadata["lastWork"] = childErr.Error()
				} else {
					state.Status = "done"
					lw := resultText
					if len(lw) > maxSnippetLen {
						lw = lw[:maxSnippetLen]
					}
					state.Metadata["lastWork"] = lw
				}
				state.Metadata["elapsed"] = elapsed
				if childSessionID != "" {
					// Append only if the early SessionInitEvent path (above) did
					// not already record this id, so conversationIds carries no
					// duplicate when the id was captured at dispatch start.
					existing, _ := state.Metadata["conversationIds"].([]interface{})
					alreadyPresent := false
					for _, v := range existing {
						if s, ok := v.(string); ok && s == childSessionID {
							alreadyPresent = true
							break
						}
					}
					if !alreadyPresent {
						state.Metadata["conversationIds"] = append(existing, childSessionID)
					}
					state.Metadata["conversationId"] = childSessionID
				}
				// Update the current dispatch entry in the structured dispatches array.
				agents.UpdateDispatchEntry(state.Metadata, agentID, state.Status, elapsed, childSessionID)
			})
			sa.EmitAgentSnapshot("dispatch_end")

			// Fire agent_end on the parent extension group.
			if extGroup := sa.ExtGroup(); extGroup != nil && !extGroup.IsEmpty() {
				utils.Log("Dispatch", fmt.Sprintf("firing agent_end key=%s name=%s id=%s status=%d", key, agentName, agentID, exitCode))
				endCtx := NewExtContext(sa)
				extGroup.FireAgentEnd(endCtx, extension.AgentInfo{
					Name: agentName,
					Task: opts.Task,
				})
			}

			// Phase 3: Emit dispatch_end telemetry on the parent session.
			sa.Emit(types.EngineEvent{
				Type:                   "engine_dispatch_end",
				DispatchAgent:          opts.Name,
				DispatchExitCode:       exitCode,
				DispatchElapsed:        elapsed,
				DispatchCost:           totalCost,
				DispatchInputTokens:    totalInputTokens,
				DispatchOutputTokens:   totalOutputTokens,
				DispatchToolCount:      toolCount,
				DispatchThinkingTokens: totalThinkingTokens,
				DispatchDepth:          childDepth,
				DispatchParentId:       currentDispatchId,
				DispatchId:             agentID,
				DispatchConversationID: childSessionID,
			})

			utils.Log("Dispatch", fmt.Sprintf(
				"dispatch complete agent=%q exitCode=%d elapsed=%.2fs cost=%.6f tools=%d session=%s",
				opts.Name, exitCode, elapsed, totalCost, toolCount, key,
			))

			return result
		}

		if opts.Background {
			// Register in the dispatch registry for recall support, child-run
			// steering, and the carry-forward allowlist. See registerDispatch.
			registerDispatch(registry, agentID, opts.Name, func() {
				recallReason = "recall_agent"
				cancelFn()
			}, child, key, currentDispatchId, childDepth, childReqID, opts.AllowedSubAgents)

			// Launch the child in a goroutine and return a stub immediately.
			//
			// The deferred recover() block is the safety backstop for the
			// "agent never reaches terminal status" failure mode. Today's
			// runChild path emits agent_end on every exit branch (normal
			// completion, child error, recall) — but any panic inside
			// runChild, startChild, the child OnNormalized callback, the
			// progress emitter, or the agent-state UpdateAgentStateByID
			// closure would otherwise kill this goroutine silently. No
			// agent_end fires, no dispatch_end telemetry is emitted, the
			// dispatch registry retains the agent name forever, and the
			// background_agents counter on engine_status stays positive
			// until the engine process restarts. The original incident
			// in conversation 1780874102870-12aee36b1e8d (see
			// docs/diagnoses or the plan file) is the textbook example.
			//
			// Recovery here synthesizes the same terminal transitions
			// that runChild's success/error/recall branches do: agent
			// status flips to "error", an agent_state snapshot fires,
			// agent_end fires on the parent extension group, and the
			// dispatch registry deregisters the name. The result is
			// that consumers see exactly the same lifecycle they would
			// for any other dispatch failure, with the panic message
			// available in lastWork for postmortem.
			go func() {
				defer cancelFn() // ensure context is cleaned up when goroutine exits
				defer func() {
					if r := recover(); r != nil {
						recoverBackgroundDispatchPanic(
							sa, registry, opts, key, agentID, agentName, r,
							childDepth, currentDispatchId,
						)
					}
				}()
				result := runChild()

				// Fire the appropriate callback.
				if recalled {
					if opts.OnRecall != nil {
						opts.OnRecall(extension.RecallInfo{
							DispatchID: agentID,
							Reason:     recallReason,
							Elapsed:    result.Elapsed,
							ToolCount:  toolCount,
						})
					}
				} else if childErr != nil || result.ExitCode != 0 {
					if opts.OnError != nil {
						opts.OnError(extension.DispatchError{
							DispatchID: agentID,
							Message:    result.Output,
							ExitCode:   result.ExitCode,
							Elapsed:    result.Elapsed,
						})
					}
				} else {
					if opts.OnComplete != nil {
						opts.OnComplete(*result)
					}
				}
			}()

			utils.Log("Dispatch", fmt.Sprintf(
				"background dispatch started agent=%q session=%s", opts.Name, key,
			))

			// Return a stub result immediately.
			return &extension.DispatchAgentResult{
				DispatchID: agentID,
				SessionID:  childReqID,
			}, nil
		}

		// Foreground (synchronous) dispatch.
		// Register in the dispatch registry so foreground dispatches are
		// recallable, counted, and steerable, matching background behavior.
		registerDispatch(registry, agentID, opts.Name, func() {
			recallReason = "recall_agent"
			cancelFn()
		}, child, key, currentDispatchId, childDepth, childReqID, opts.AllowedSubAgents)

		defer cancelFn() // clean up the context
		result := runChild()

		if childErr != nil {
			return result, childErr
		}
		return result, nil
	}
}

// fireLifecycleCallbacks and truncate live in dispatch_lifecycle_callbacks.go,
// and loadChildExtension and startChild live in dispatch_child_setup.go (all
// same package) to keep this file under the 800-line cap.

// buildChildElicitFn adapts an OnChildQuestion dispatcher callback into the
// backend.RunConfig.ChildElicitFn shape the runloop calls. When the child
// run's AskUserQuestion fires, the runloop invokes the returned function with
// the question text; this wraps it in a DispatchChildQuestionInfo stamped with
// the dispatch's name, id, and depth, then forwards to the dispatcher. Kept as
// a package-level function (rather than an inline closure) so the wiring is
// directly unit-testable without standing up a full child run.
func buildChildElicitFn(fn func(extension.DispatchChildQuestionInfo) (string, bool, error), name, dispatchID string, depth int) func(string) (string, bool, error) {
	return func(question string) (string, bool, error) {
		return fn(extension.DispatchChildQuestionInfo{
			Name:       name,
			DispatchID: dispatchID,
			Question:   question,
			Depth:      depth,
		})
	}
}
