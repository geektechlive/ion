package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/telemetry"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// handleNormalizedEvent translates a NormalizedEvent into an EngineEvent
// and forwards it through the Manager's event callback.
func (m *Manager) handleNormalizedEvent(runID string, event types.NormalizedEvent) {
	key := m.keyForRun(runID)
	if key == "" {
		// No session resolves for this runID — the event cannot be routed and
		// is dropped. This is expected only AFTER a run's terminal point (the
		// binding is cleared in handleRunExit). A drop for a runID that is still
		// live is a routing defect: log it with the event type so a silent loss
		// is reconstructable from engine.log (this path was previously a silent
		// return — the blind spot that hid the dropped PlanModeChangedEvent).
		utils.Warn("Session", fmt.Sprintf("normalized event DROPPED: no key for runID=%s type=%T (post-exit is expected; mid-run indicates a routing defect)", runID, event.Data))
		return
	}

	utils.Debug("Session", fmt.Sprintf("normalized event: key=%s runID=%s type=%T", key, runID, event.Data))

	// Look up session once for all downstream hook firing.
	m.mu.RLock()
	s, sOk := m.sessions[key]
	m.mu.RUnlock()

	// Fire CLI backend turn lifecycle hooks BEFORE the translate/drop gate.
	// TaskUpdateEvent (assistant message complete) has no client-facing
	// EngineEvent translation and would be dropped by the ee.Type == ""
	// check below, but it is the signal for turn_end.
	m.fireCliTurnHooks(s, key, sOk, event)

	// Capture the conversation/session ID as early as possible. The API
	// backend emits a SessionInitEvent right after loadOrCreateConversation
	// so the session manager learns the ID before any tool call or dispatch
	// completes. Without this, s.conversationID is empty during the first
	// run, which causes dispatch persistence (appendConversationEntry) to
	// silently skip writing agent_dispatch entries.
	if init, ok := event.Data.(*types.SessionInitEvent); ok && init.SessionID != "" {
		m.mu.Lock()
		if s2, ok2 := m.sessions[key]; ok2 && s2.conversationID == "" {
			s2.conversationID = init.SessionID
			utils.Log("Session", fmt.Sprintf("captured conversationID=%s from SessionInitEvent key=%s", init.SessionID, key))

			// Initialize session memory for the newly created conversation.
			// On resumed sessions this is already done in StartSession; here
			// we cover the fresh-conversation path where the backend assigns
			// the conversation ID during the first run.
			memoryDisabled := m.config != nil && m.config.Compaction != nil &&
				m.config.Compaction.MemoryEnabled != nil && !*m.config.Compaction.MemoryEnabled
			if s2.sessionMemory == nil && !memoryDisabled {
				home, _ := os.UserHomeDir()
				convDir := filepath.Join(home, ".ion", "conversations")
				sm := NewSessionMemory(init.SessionID, convDir, nil)
				sm.Start()
				s2.sessionMemory = sm
				utils.Log("Session", fmt.Sprintf("created session memory for new conv=%s key=%s", init.SessionID, key))
			}
		}
		m.mu.Unlock()
	}

	contextWindow := conversation.DefaultContext
	m.mu.RLock()
	if s, sOk2 := m.sessions[key]; sOk2 && s.lastContextWindow > 0 {
		contextWindow = s.lastContextWindow
	}
	m.mu.RUnlock()

	ee := translateToEngineEvent(event, contextWindow)
	if ee.Type == "" {
		utils.Debug("Session", fmt.Sprintf("dropping unhandled normalized event type: %T", event.Data))
		return
	}

	// The task_complete → engine_status translation stamps the
	// backend-reported sessionID (claude's UUID for the CLI backend) onto
	// Fields.SessionID. Substitute Ion's stable conversationID so the
	// client-facing session id is consistent with every other surface
	// (handleRunExit idle status, buildSessionStatusMirror, ListSessions)
	// and never leaks a claude UUID that has no Ion conversation file. For
	// the API backend the two values are equal, so this is a no-op there.
	// translateToEngineEvent is a pure function with no session access, so
	// the substitution must happen here where the manager holds the session.
	if ee.Type == "engine_status" && ee.Fields != nil {
		m.mu.RLock()
		if s2, ok2 := m.sessions[key]; ok2 && s2.conversationID != "" {
			if ee.Fields.SessionID != s2.conversationID {
				utils.Debug("Session", fmt.Sprintf("task_complete status: substituting Ion conversationID=%s for backend sessionID=%s key=%s", s2.conversationID, ee.Fields.SessionID, key))
			}
			ee.Fields.SessionID = s2.conversationID
		}
		m.mu.RUnlock()
	}

	m.emit(key, ee)

	// Track plan mode changes so re-entering plan mode triggers reentry
	// detection in SendPrompt. We do this here (rather than in the pure
	// translateToEngineEvent) because we need access to the session manager.
	if pmc, ok := event.Data.(*types.PlanModeChangedEvent); ok {
		if !pmc.Enabled {
			// Model called ExitPlanMode: record the exit so that if the
			// session is later re-entered into plan mode, the reentry
			// prompt fires.
			m.MarkPlanModeExited(key)
		} else if pmc.PlanFilePath != "" {
			// Model called EnterPlanMode: keep the manager's session state in
			// sync with the run's state so the next SendPrompt sees the correct
			// planFilePath and planMode flag. Without this the manager's view
			// diverges from the backend run's view across run boundaries.
			m.mu.Lock()
			if s2, ok2 := m.sessions[key]; ok2 {
				s2.planMode = true
				s2.planFilePath = pmc.PlanFilePath
				utils.Info("PlanMode", fmt.Sprintf("event_translation: key=%s model entered plan mode planFile=%s", key, pmc.PlanFilePath))
			}
			m.mu.Unlock()
		}
	}

	// Per ADR-003, the model calling ExitPlanMode surfaces as a
	// PlanProposalEvent{Kind:"exit"} (a workflow proposal), NOT a
	// PlanModeChangedEvent{Enabled:false} (a confirmed state change). The
	// CLI backend emits this on the model's ExitPlanMode tool call, and the
	// API backend emits it from interceptExitPlanMode. Record the exit so
	// reentry detection fires when plan mode is re-enabled — mirroring the
	// PlanModeChangedEvent{Enabled:false} branch above. Idempotent with the
	// SetPlanMode(false) user-approval chokepoint path (both set
	// hasExitedPlanMode=true).
	if pp, ok := event.Data.(*types.PlanProposalEvent); ok && pp.Kind == "exit" {
		m.MarkPlanModeExited(key)
	}

	// Track last-known context usage on the session so subsequent
	// engine_status emissions carry the latest values.
	if ee.EndUsage != nil && ee.EndUsage.ContextPercent > 0 {
		m.mu.Lock()
		if s, ok2 := m.sessions[key]; ok2 {
			s.lastContextPct = ee.EndUsage.ContextPercent
		}
		m.mu.Unlock()
	}

	// G34: Fire tool_start/tool_end extension hooks and track tool inputs
	// for Agent tool_call dispatch.
	if sOk && s.extGroup != nil && !s.extGroup.IsEmpty() {
		ctx := m.newExtContext(s, key)
		switch e := event.Data.(type) {
		case *types.ToolCallEvent:
			_ = s.extGroup.FireToolStart(ctx, extension.ToolStartInfo{
				ToolName: e.ToolName,
				ToolID:   e.ToolID,
			})
			// Track tool metadata for Agent tool_call hook
			m.mu.Lock()
			if s.cliToolMeta == nil {
				s.cliToolMeta = make(map[string]toolMeta)
				s.cliToolInputs = make(map[string]string)
				s.cliToolIndexID = make(map[int]string)
			}
			s.cliToolMeta[e.ToolID] = toolMeta{name: e.ToolName, index: e.Index}
			s.cliToolIndexID[e.Index] = e.ToolID
			s.cliLastToolID = e.ToolID
			m.mu.Unlock()

		case *types.ToolCallUpdateEvent:
			// Accumulate partial input for tool_call hook.
			// ToolCallUpdateEvent.ToolID is always "" from the normalizer because
			// content_block_delta events don't carry a toolID. Fall back to the
			// last-started tool so the input accumulates under the right key.
			m.mu.Lock()
			if s.cliToolInputs != nil {
				key := e.ToolID
				if key == "" {
					key = s.cliLastToolID
				}
				s.cliToolInputs[key] += e.PartialInput
			}
			m.mu.Unlock()

		case *types.ToolCallCompleteEvent:
			// Fire tool_call hook for Agent tool calls so extensions can see
			// which sub-agent is being dispatched.
			m.mu.Lock()
			toolID := s.cliToolIndexID[e.Index]
			meta := s.cliToolMeta[toolID]
			accumulated := s.cliToolInputs[toolID]
			delete(s.cliToolInputs, toolID)
			delete(s.cliToolMeta, toolID)
			delete(s.cliToolIndexID, e.Index)
			m.mu.Unlock()

			if meta.name == "Agent" && accumulated != "" {
				var input map[string]interface{}
				if json.Unmarshal([]byte(accumulated), &input) == nil {
					_, _ = s.extGroup.FireToolCall(ctx, extension.ToolCallInfo{
						ToolName: "Agent",
						ToolID:   toolID,
						Input:    input,
					})
				}
			}

		case *types.ToolResultEvent:
			_ = e // suppress unused
			_ = s.extGroup.FireToolEnd(ctx)
		}
	}

	// Fire on_error extension hook
	if sOk && s.extGroup != nil && !s.extGroup.IsEmpty() {
		if errEv, ok := event.Data.(*types.ErrorEvent); ok {
			errCtx := m.newExtContext(s, key)
			_ = s.extGroup.FireOnError(errCtx, extension.ErrorInfo{
				Message:      errEv.ErrorMessage,
				ErrorCode:    errEv.ErrorCode,
				Category:     classifyErrorCategory(errEv.ErrorCode),
				Retryable:    errEv.Retryable,
				RetryAfterMs: errEv.RetryAfterMs,
				HttpStatus:   errEv.HttpStatus,
			})
		}
	}

	// TaskComplete also emits engine_message_end with usage
	if tc, ok := event.Data.(*types.TaskCompleteEvent); ok {
		var pct int
		if tc.Usage.InputTokens != nil {
			pct = *tc.Usage.InputTokens * 100 / contextWindow
			if pct > 100 {
				pct = 100
			}
		}
		m.mu.Lock()
		if s2, ok2 := m.sessions[key]; ok2 {
			if pct > 0 {
				s2.lastContextPct = pct
			}
			if tc.CostUsd > 0 {
				s2.lastTotalCost = tc.CostUsd
			}
			// Capture pending denials so ReconcileState can re-emit them
			// on the engine_status snapshot a re-attaching consumer
			// requests. Cleared on next prompt dispatch (see
			// prompt_dispatch.go). The full PermissionDenials slice
			// from the task_complete payload is retained verbatim;
			// consumer-side filtering or interpretation is out of
			// scope for the engine.
			//
			// Snapshot semantics: this assignment REPLACES whatever was
			// previously retained. The most recent task_complete is the
			// authoritative truth about what (if anything) is still blocked.
			// An empty PermissionDenials slice correctly clears the
			// retained state — a task that completed cleanly has no
			// outstanding denials to re-emit.
			s2.lastPermissionDenials = tc.PermissionDenials
			utils.Log("Session", fmt.Sprintf("task_complete: key=%s retained %d permission_denials for reconcile", key, len(tc.PermissionDenials)))

			// Emit a run-level telemetry event. This is the one place every
			// backend's TaskCompleteEvent converges, so a single guarded
			// emission here gives uniform run-level coverage across all
			// backends — including CliBackend, which emits no per-call
			// telemetry spans of its own (ApiBackend keeps its finer-grained
			// llm.call / tool.execute spans regardless). Additive only:
			// guarded on a non-nil collector, and the collector itself is a
			// no-op when telemetry is disabled. The model comes from
			// s2.lastModel (set in prompt_dispatch when the run started); the
			// cost/duration/turn/usage fields come straight from the event.
			if s2.telemetry != nil {
				payload := map[string]any{
					"model":                    s2.lastModel,
					"costUsd":                  tc.CostUsd,
					"durationMs":               tc.DurationMs,
					"numTurns":                 tc.NumTurns,
					"inputTokens":              derefInt(tc.Usage.InputTokens),
					"outputTokens":             derefInt(tc.Usage.OutputTokens),
					"cacheReadInputTokens":     derefInt(tc.Usage.CacheReadInputTokens),
					"cacheCreationInputTokens": derefInt(tc.Usage.CacheCreationInputTokens),
				}
				s2.telemetry.Event(telemetry.RunComplete, payload, nil)
				utils.Log("Session", fmt.Sprintf("run.complete telemetry emitted: key=%s model=%s costUsd=%f numTurns=%d", key, s2.lastModel, tc.CostUsd, tc.NumTurns))
			}
		}
		m.mu.Unlock()
		m.emit(key, types.EngineEvent{
			Type: "engine_message_end",
			EndUsage: &types.MessageEndUsage{
				InputTokens:    derefInt(tc.Usage.InputTokens),
				OutputTokens:   derefInt(tc.Usage.OutputTokens),
				ContextPercent: pct,
				Cost:           tc.CostUsd,
			},
		})
	}
}

// handleRunExit is called when a backend run exits.
func (m *Manager) handleRunExit(runID string, code *int, signal *string, sessionID string) {
	key := m.keyForRun(runID)
	if key == "" {
		return
	}

	codeStr, sigStr := "nil", "nil"
	if code != nil {
		codeStr = fmt.Sprintf("%d", *code)
	}
	if signal != nil {
		sigStr = *signal
	}
	utils.Info("Session", fmt.Sprintf("handleRunExit: key=%s runID=%s code=%s signal=%s sessionID=%s", key, runID, codeStr, sigStr, sessionID))

	var nextPrompt *pendingPrompt
	var bgCount int
	var ionConvID string
	m.mu.Lock()
	// Authoritative terminal point: clear the runID -> key routing binding
	// under the lock, unconditionally (even if the session was already torn
	// down) so the binding can never leak. After this, a late event for the
	// same runID correctly resolves to "" and is dropped.
	m.unbindRunLocked(runID)
	if s, ok := m.sessions[key]; ok {
		s.requestID = ""
		// Ion's durable conversation-file identity, captured under the lock
		// for use in persistTerminalDispatches below. This is NOT the
		// backend-reported sessionID (which is claude's UUID for the CLI
		// backend and has no Ion files).
		ionConvID = s.conversationID
		// Preserve completed agent states (done/error/cancelled) so their
		// conversation history survives for post-run inspection and tab
		// persistence. Also preserve running states that correspond to active
		// background dispatches — those agents are legitimately still running.
		// Only clear running states that are stale (no live dispatch backing them).
		//
		// Preservation keys on BOTH the live dispatch IDs and names. The ID set
		// covers engine-managed dispatch slots at every depth (the agent-state
		// store keys those slots by their unique dispatch ID, and a nested
		// depth-2+ dispatch's name collapses under name-only keying, so it would
		// be swept and its terminal UpdateStateByID would land nowhere — the
		// "agent stuck running" defect). The name set covers extension-roster
		// rows that carry no engine dispatch ID. bgCount is the count of live
		// dispatch instances (by ID), not distinct names.
		if s.dispatchRegistry != nil {
			activeIDs := s.dispatchRegistry.ActiveIDs()
			activeNames := s.dispatchRegistry.ActiveNames()
			bgCount = len(activeIDs)
			if len(activeIDs) > 0 || len(activeNames) > 0 {
				utils.Log("Session", fmt.Sprintf("handleRunExit: preserving %d live dispatch(es) by id=%v name=%v", bgCount, activeIDs, activeNames))
				s.agents.ClearRunningStatesExceptIDsOrNames(activeIDs, activeNames)
			} else {
				s.agents.ClearRunningStates()
			}
		} else {
			s.agents.ClearRunningStates()
		}
		// Capture the backend-reported sessionID into cliSessionID — claude's
		// native session UUID is what `--resume` needs on the next CLI run.
		// CRITICAL: do NOT write it into s.conversationID. conversationID is
		// Ion's durable conversation-file identity; overwriting it with a
		// claude UUID corrupts compaction, export, /clear, tree navigation,
		// and the client-facing session id (all keyed on the Ion id). For the
		// API backend the reported sessionID equals s.conversationID already,
		// so storing it in cliSessionID is inert there (the API backend never
		// reads CliResumeSessionID).
		if sessionID != "" {
			s.cliSessionID = sessionID
			utils.Log("Session", fmt.Sprintf("handleRunExit: captured cliSessionID=%s key=%s (conversationID=%s unchanged)", sessionID, key, s.conversationID))
		} else {
			utils.Log("Session", fmt.Sprintf("handleRunExit: no sessionID reported by backend key=%s (cliSessionID unchanged=%s)", key, s.cliSessionID))
		}
		if len(s.promptQueue) > 0 {
			next := s.promptQueue[0]
			s.promptQueue = s.promptQueue[1:]
			nextPrompt = &next
		}
	}
	m.mu.Unlock()

	// Persist any terminal dispatch entries to the conversation file.
	// This runs AFTER the backend's final save (which fires before OnExit)
	// so the load-append-save cycle won't be overwritten by a subsequent
	// backend save. Only terminal states (done/error/cancelled) with
	// dispatch metadata (task, agent type) are persisted. Keyed on Ion's
	// conversationID (the file basename) — never the backend-reported
	// sessionID, which for the CLI backend is claude's UUID with no Ion file.
	m.persistTerminalDispatches(key, ionConvID)

	// Flush a deferred key->conversationId binding now that a run has exited
	// and the backend's final save has landed. A freshly pre-minted session
	// deferred its binding at StartSession (bindingPending) to avoid leaving a
	// phantom binding for a session that never saved. We only write the binding
	// if the conversation file actually exists — a run that exited without ever
	// producing a turn (no save) leaves bindingPending set and writes nothing,
	// so the next restart won't try to resume an empty id. (#230/#231)
	m.flushPendingBinding(key, ionConvID)

	// Emit updated agent state snapshot after clearing running agents.
	// Completed agents (done/error/cancelled) are preserved so their
	// conversation history survives for post-run inspection. The merged
	// snapshot includes both extension-managed roster entries and any
	// retained engine-managed agents.
	//
	// Engine contract: `engine_agent_state` is a complete snapshot.
	// See docs/architecture/agent-state.md.
	m.mu.RLock()
	var runExitSnapshot []types.AgentStateUpdate
	if s, ok := m.sessions[key]; ok {
		runExitSnapshot = s.agents.MergedSnapshot()
	}
	m.mu.RUnlock()
	utils.Log("Session", fmt.Sprintf("agent_snapshot_emitted key=%s count=%d reason=run_exit", key, len(runExitSnapshot)))
	m.emit(key, types.EngineEvent{
		Type:   "engine_agent_state",
		Agents: runExitSnapshot,
	})

	// Clear any stale working message before transitioning to idle
	m.emit(key, types.EngineEvent{Type: "engine_working_message", EventMessage: ""})

	// When background dispatches are still running, include the count so
	// clients can keep the tab status active and interrupt button visible
	// even though the parent LLM turn has ended.
	//
	// buildIdleStatusFields reads the retained context/cost state (pct, cw,
	// model, cost, sessionID) under m.mu and stamps bgCount directly. The
	// same helper is used by emitDispatchCountStatus so both emission sites
	// carry identical fields — preventing drift between the run-exit snapshot
	// and the post-deregister correction.
	m.mu.RLock()
	var exitSession *engineSession
	if s2, ok := m.sessions[key]; ok {
		exitSession = s2
	}
	m.mu.RUnlock()

	if bgCount > 0 {
		utils.Log("Session", fmt.Sprintf("handleRunExit: emitting idle with backgroundAgents=%d key=%s", bgCount, key))
	}
	var idleFields *types.StatusFields
	if exitSession != nil {
		idleFields = m.buildIdleStatusFields(exitSession, key, bgCount)
	} else {
		idleFields = &types.StatusFields{Label: key, State: "idle", BackgroundAgents: bgCount}
	}
	m.emit(key, types.EngineEvent{
		Type:   "engine_status",
		Fields: idleFields,
	})

	// Classify the exit. A cooperative cancel — code==0 with the "cancelled"
	// signal — is a CLEAN, recoverable exit, not a death: the run was
	// interrupted on purpose (user/auto abort, or a turn/tool hook cancelling
	// the run), the conversation is intact, and the session is immediately
	// reusable on the next prompt. Emitting engine_dead for it would overload
	// the event with a second, contradictory meaning and make a deliberately
	// interrupted run look like a crash (the 1782088921498-960b064fe896
	// incident, where the stuck-tab watchdog's abort produced a false "tab
	// died" for a perfectly recoverable run).
	//
	// engine_dead is reserved for ABNORMAL termination: a non-zero exit code,
	// or any signal other than the cooperative "cancelled" (e.g. SIGKILL,
	// SIGSEGV, or the watchdog's "cancelled-forced" hard kill). Those are real
	// deaths a consumer must surface. Narrowing engine_dead's trigger set is a
	// contract change ratified by ADR-013 (docs/architecture/adr/
	// 013-engine-dead-clean-cancel.md); see also ADR-003 for the precedent.
	cleanCancel := (code == nil || *code == 0) && signal != nil && *signal == "cancelled"
	abnormalExit := (code != nil && *code != 0) || (signal != nil && *signal != "cancelled")

	// Descendant teardown runs for ANY non-normal exit (clean cancel OR
	// abnormal death), independent of whether we emit engine_dead. A clean
	// cancel can arrive straight from the runloop (a turn_start / turn_end /
	// tool hook cancelling the run) WITHOUT flowing through SendAbort, so the
	// SendAbort-side abortAllDescendants is not guaranteed to have fired.
	// Reaping here ensures dispatched children never outlive a cancelled
	// parent regardless of the cancel's origin.
	if cleanCancel || abnormalExit {
		m.abortAllDescendants(key, fmt.Sprintf("parent run exit code=%s signal=%s", codeStr, sigStr))
	}

	if abnormalExit {
		utils.Warn("Session", fmt.Sprintf("emitting engine_dead: key=%s code=%s signal=%s", key, codeStr, sigStr))
		m.emit(key, types.EngineEvent{
			Type:     "engine_dead",
			ExitCode: code,
			Signal:   signal,
		})
	} else if cleanCancel {
		utils.Info("Session", fmt.Sprintf("clean cancel (no engine_dead): key=%s code=%s signal=%s", key, codeStr, sigStr))
	}

	// Auto-respawn any extension hosts whose subprocess died during the
	// run. Now that the run has finished we can rebuild safely without
	// mid-turn hook interleaving.
	m.respawnDeadExtensions(key)

	// Dispatch queued prompt outside the lock
	if nextPrompt != nil {
		utils.Debug("Session", fmt.Sprintf("dispatching queued prompt: key=%s", key))
		go func() {
			var ov *PromptOverrides
			if nextPrompt.model != "" || nextPrompt.maxTurns > 0 || nextPrompt.maxBudgetUsd > 0 || len(nextPrompt.extensions) > 0 || nextPrompt.noExtensions || len(nextPrompt.attachments) > 0 || nextPrompt.implementationPhase || nextPrompt.thinkingEffort != "" {
				ov = &PromptOverrides{
					Model:               nextPrompt.model,
					MaxTurns:            nextPrompt.maxTurns,
					MaxBudgetUsd:        nextPrompt.maxBudgetUsd,
					Extensions:          nextPrompt.extensions,
					NoExtensions:        nextPrompt.noExtensions,
					Attachments:         nextPrompt.attachments,
					ImplementationPhase: nextPrompt.implementationPhase,
					ThinkingEffort:      nextPrompt.thinkingEffort,
				}
			}
			if err := m.SendPrompt(key, nextPrompt.text, ov); err != nil {
				utils.Error("Session", "queued prompt failed: "+err.Error())
			}
		}()
	}
}

// handleRunError is called when a backend run encounters an error.
// The error event is already emitted by ApiBackend.emitError via the
// NormalizedEvent pipeline (with structured ProviderError fields). This
// callback exists for logging and potential future coordination.
func (m *Manager) handleRunError(runID string, err error) {
	key := m.keyForRun(runID)
	if key == "" {
		return
	}
	utils.Error("Session", fmt.Sprintf("handleRunError: key=%s runID=%s err=%s", key, runID, err.Error()))
	// Reap descendants so a dispatched child does not continue running
	// (and billing model time) after the parent loop has died.
	m.abortAllDescendants(key, fmt.Sprintf("parent run error: %s", err.Error()))
}

// classifyErrorCategory maps an error code to an extension ErrorCategory.
func classifyErrorCategory(code string) extension.ErrorCategory {
	switch code {
	case "rate_limit", "overloaded", "auth", "timeout", "network",
		"stale_connection", "invalid_model", "stream_truncated",
		"invalid_request", "prompt_too_long", "content_filter",
		"media_error", "pdf_error", "unknown":
		return extension.ErrorCategoryProvider
	default:
		return extension.ErrorCategoryProvider
	}
}
