package session

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dsswift/ion/engine/internal/conversation"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/mcp"
	"github.com/dsswift/ion/engine/internal/permissions"
	"github.com/dsswift/ion/engine/internal/providers"
	"github.com/dsswift/ion/engine/internal/resource"
	"github.com/dsswift/ion/engine/internal/session/agents"
	"github.com/dsswift/ion/engine/internal/session/extcontext"
	"github.com/dsswift/ion/engine/internal/session/pending"
	"github.com/dsswift/ion/engine/internal/skills"
	"github.com/dsswift/ion/engine/internal/telemetry"
	"github.com/dsswift/ion/engine/internal/tools"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// StartSessionResult carries information about the session after a StartSession call.
type StartSessionResult struct {
	Existed        bool   `json:"existed"`
	ConversationID string `json:"conversationId,omitempty"`
}

// StartSession creates a new session with the given config.
func (m *Manager) StartSession(key string, config types.EngineConfig) (*StartSessionResult, error) {
	utils.Info("Session", fmt.Sprintf("StartSession: key=%s dir=%s extensions=%d", key, config.WorkingDirectory, len(config.Extensions)))
	m.mu.Lock()

	if s, exists := m.sessions[key]; exists {
		convID := s.conversationID
		needsExtensions := len(config.Extensions) > 0 && (s.extGroup == nil || s.extGroup.IsEmpty())
		m.mu.Unlock()

		// Re-register extensions when the session was restored without them
		// (e.g. daemon restart where the extension subprocess was not persisted).
		if needsExtensions {
			utils.Log("Session", fmt.Sprintf("StartSession: key=%s re-registering %d extensions on existing session", key, len(config.Extensions)))
			m.loadAndWireExtensions(s, key, config)
		}

		utils.Log("Session", fmt.Sprintf("StartSession: key=%s already exists (idempotent, conversationID=%s)", key, convID))
		return &StartSessionResult{Existed: true, ConversationID: convID}, nil
	}

	// Resolve the conversation ID for this session. When the caller supplies an
	// explicit SessionID it wins; otherwise the binding store and the
	// ForceNewConversation flag decide between resume and fresh-mint. See
	// resolveConversationID in session_bindings.go for the full decision tree
	// and logging. The backend's loadOrCreateConversation handles a pre-set id:
	// it tries Load, gets ErrNotFound (no file yet), and calls CreateConversation
	// with this ID — so the conversation file will use this same ID. (#230/#231)
	convID := resolveConversationID(bindingsPath(), key, config)

	// Whether the resolved conversation already has a backing file. A genuine
	// resume (file present) gets its binding written immediately below for
	// restart resilience; a freshly pre-minted id (no file) DEFERS the binding
	// until the conversation is first saved, so a started-but-never-saved
	// session never leaves a phantom binding. (#230/#231)
	convExists := conversation.Exists(convID, "")

	s := &engineSession{
		key:              key,
		config:           config,
		conversationID:   convID,
		bindingPending:   !convExists,
		agents:           agents.NewRegistry(),
		childPIDs:        make(map[int]struct{}),
		pending:          pending.New(),
		maxQueueDepth:    32,
		dispatchRegistry: extcontext.NewDispatchRegistry(),
		resourceBroker:   resource.NewBroker(),
	}

	// Initialize the session's cancellation root before any run or
	// dispatch can be launched. Every cancellable operation spawned for
	// this session derives from this root, so SendAbort / StopSession can
	// cancel the whole in-flight tree in one call. See
	// session_root_context.go.
	s.newSessionRootContext()

	// Initialize process registry for extension-spawned subprocesses.
	// If the PID-file directory cannot be created, log and continue with a
	// nil registry — downstream call sites (extcontext.go) already guard
	// with `if reg := sa.ProcRegistry(); reg != nil`, so extensions that
	// would have used it degrade to no-op instead of silently failing.
	home, _ := os.UserHomeDir()
	pidsDir := filepath.Join(home, ".ion", "agent-pids")
	if reg, err := extension.NewProcessRegistry(pidsDir); err != nil {
		utils.Log("session", fmt.Sprintf("StartSession key=%s: process registry unavailable: %v", key, err))
		s.procRegistry = nil
	} else {
		s.procRegistry = reg
	}

	// Wire permissions from config (default allow-all when no policy configured)
	if m.config != nil && m.config.Permissions != nil {
		s.permEngine = permissions.NewEngine(m.config.Permissions)
	} else {
		s.permEngine = permissions.NewEngine(&permissions.DefaultPolicy)
	}
	// G01: Wire LLM classifier for "ask" mode
	if s.permEngine != nil && m.config != nil && m.config.Permissions != nil && m.config.Permissions.Mode == "ask" {
		s.permEngine.SetClassifier(permissions.NewLlmClassifier(""))
	}

	// Wire telemetry from config
	if m.config != nil && m.config.Telemetry != nil && m.config.Telemetry.Enabled {
		s.telemetry = telemetry.NewCollector(*m.config.Telemetry)
	}

	m.sessions[key] = s

	m.mu.Unlock()

	// Persist the key->conversationId binding for restart resilience (B2 fix
	// for issue #230) ONLY for a genuine resume — a conversation whose file
	// already exists on disk. For a freshly pre-minted id (no file yet) the
	// binding is DEFERRED until the conversation is first saved (flushed in
	// handleRunExit). This prevents a started-but-never-saved session from
	// leaving a "phantom" binding that a later restart would resume into an
	// empty conversation — the failure mode that orphaned real history across
	// the desktop restart. (#230/#231)
	if !s.bindingPending {
		saveBinding(bindingsPath(), key, convID)
	} else {
		utils.Log("Session", fmt.Sprintf("StartSession: key=%s deferring binding for pre-minted conversationID=%s until first save", key, convID))
	}

	// Rehydrate agent dispatch state from the conversation file if the
	// session is resuming an existing conversation. This runs before
	// extensions fire session_start so the agent registry is pre-populated
	// with completed dispatches. When the extension later emits its fresh
	// roster, MergedSnapshot deduplicates: engine-managed entries (with
	// task, conversationId, elapsed) win over the extension's idle entries.
	if s.conversationID != "" {
		m.rehydrateDispatchState(s, key)

		// Seed lastModel from the conversation file so ReconcileState emits
		// the correct model before any prompt dispatches. Without this, a
		// resumed session emits model="" on reconcile, causing the desktop to
		// fall back to its preference default (which may differ from the
		// conversation's actual model). This also seeds lastContextWindow so
		// the context-percent denominator is correct from the first status.
		if convModel, err := conversation.LoadLlmHeaderModel(s.conversationID, ""); err == nil && convModel != "" {
			ctxWindow := conversation.DefaultContext
			if info := providers.GetModelInfo(convModel); info != nil {
				ctxWindow = info.ContextWindow
			}
			// Seed lastContextPct from the persisted conversation so the initial
			// idle engine_status reports the true usage instead of 0%. Without
			// this a resumed conversation shows an empty context bar until the
			// first prompt's usage event lands. Load the full conversation to
			// compute usage against the resolved context window.
			seededPct := 0
			if conv, lerr := conversation.Load(s.conversationID, ""); lerr == nil {
				usage := conversation.GetContextUsage(conv, ctxWindow)
				if usage.Percent > 0 {
					seededPct = usage.Percent
				}
			}
			m.mu.Lock()
			s.lastModel = convModel
			s.lastContextWindow = ctxWindow
			if seededPct > 0 {
				s.lastContextPct = seededPct
			}
			m.mu.Unlock()
			utils.Log("Session", fmt.Sprintf("StartSession: key=%s seeded lastModel=%s contextWindow=%d contextPct=%d from conversation=%s", key, convModel, ctxWindow, seededPct, s.conversationID))
		} else if err != nil {
			utils.Debug("Session", fmt.Sprintf("StartSession: key=%s could not load conversation model conv=%s err=%v", key, s.conversationID, err))
		}

		// Initialize session memory for resumed conversations. The memory
		// file (if it exists) is loaded from disk so the first compaction
		// on this session can use the pre-existing summary as a zero-cost
		// context restoration source. The memory updater starts via
		// Start() and will be stopped by StopSession.
		memoryDisabled := m.config != nil && m.config.Compaction != nil &&
			m.config.Compaction.MemoryEnabled != nil && !*m.config.Compaction.MemoryEnabled
		if !memoryDisabled {
			home, _ := os.UserHomeDir()
			convDir := filepath.Join(home, ".ion", "conversations")
			sm := NewSessionMemory(s.conversationID, convDir, nil)
			if sm.LoadMemory() {
				utils.Log("Session", fmt.Sprintf("StartSession: key=%s loaded session memory for conv=%s", key, s.conversationID))
			}
			sm.Start()
			m.mu.Lock()
			s.sessionMemory = sm
			m.mu.Unlock()
		} else {
			utils.Log("Session", fmt.Sprintf("StartSession: key=%s session memory disabled by config", key))
		}
	}

	// Signal that session startup is in progress so consumers can mirror
	// loading state. Events flow through the socket broadcast independently
	// of the request-response ACK, so consumers receive these before
	// StartSession returns.
	m.emit(key, types.EngineEvent{
		Type:   "engine_status",
		Fields: &types.StatusFields{Label: key, State: "starting"},
	})

	// Load extensions if configured (outside lock -- subprocess may block)
	if len(config.Extensions) > 0 {
		m.loadAndWireExtensions(s, key, config)
	}

	// Load skills from default paths
	skillPaths := skills.IonSkillPaths()
	for _, dir := range []string{skillPaths.User, skillPaths.Project} {
		loaded, err := skills.LoadSkillDirectory(dir, nil)
		if err == nil {
			for _, sk := range loaded {
				skills.RegisterSkill(sk)
			}
		}
	}
	// Load Claude Code–style skills from ~/.claude/skills (one subdir per
	// skill, each with a SKILL.md file). Only attempted when the ClaudeCompat
	// flag is set on the engine config. A missing directory is a silent no-op
	// (returns nil, nil).
	if config.ClaudeCompat {
		if claudeSkills, err := skills.LoadClaudeSkillsDirectory(skillPaths.ClaudeUser); err == nil {
			for _, sk := range claudeSkills {
				skills.RegisterSkill(sk)
			}
		}
	} else {
		utils.Debug("Session", "skipping ~/.claude/skills/ (claudeCompat not set)")
	}
	if names := skills.ListSkillNames(); len(names) > 0 {
		utils.Log("Session", fmt.Sprintf("loaded %d skills: %v", len(names), names))
		// Refresh the Skill tool's description so the model's tool manifest
		// lists the available skills (with their when_to_use hints). This
		// must run after all skills are registered; RefreshSkillToolDescription
		// re-registers the Skill tool with a freshly-built manifest.
		tools.RefreshSkillToolDescription()
	}

	// Connect MCP servers from config (outside lock)
	if m.config != nil && len(m.config.McpServers) > 0 {
		m.emit(key, types.EngineEvent{
			Type:         "engine_working_message",
			EventMessage: "Connecting MCP servers...",
		})
		for name, mcpCfg := range m.config.McpServers {
			conn, err := mcp.Connect(name, mcpCfg)
			if err != nil {
				utils.Log("Session", fmt.Sprintf("MCP connect %s failed: %s", name, err))
				continue
			}
			m.mu.Lock()
			// Guard against session disposal/replacement while Connect() was
			// blocking. If the session is gone or has been replaced, close the
			// freshly-opened connection immediately to avoid a file-descriptor
			// leak.
			if cur, ok := m.sessions[key]; !ok || cur != s {
				m.mu.Unlock()
				_ = conn.Close()
				utils.Log("Session", fmt.Sprintf("MCP %s: session %s disposed during connect — closing leaked conn", name, key))
				continue
			}
			s.mcpConns = append(s.mcpConns, conn)
			m.mu.Unlock()
			utils.Log("Session", fmt.Sprintf("MCP server %s connected (%d tools)", name, len(conn.Tools())))
		}
	}

	m.emit(key, types.EngineEvent{
		Type:         "engine_working_message",
		EventMessage: "",
	})
	// Emit the initial idle status through emitStatusSnapshot so the payload
	// carries the seeded contextPercent / contextWindow / model rather than
	// hardcoded zeros. On a resumed conversation lastContextPct is seeded above
	// from the conversation file, so the desktop binds the correct usage from
	// the first status rather than showing 0% until the first prompt.
	m.emitStatusSnapshot(key, "start_session")

	return &StartSessionResult{Existed: false, ConversationID: s.conversationID}, nil
}

// loadAndWireExtensions loads extension subprocesses, wires their hooks and
// callbacks, and fires session_start. Safe to call on both new and existing
// sessions — the caller must ensure the session does not already have a
// loaded extension group.
func (m *Manager) loadAndWireExtensions(s *engineSession, key string, config types.EngineConfig) {
	extPaths := config.Extensions
	group := extension.NewExtensionGroup()
	for _, extPath := range extPaths {
		m.emit(key, types.EngineEvent{
			Type:         "engine_working_message",
			EventMessage: fmt.Sprintf("Loading extension: %s", filepath.Base(filepath.Dir(extPath))),
		})
		host := extension.NewHost()
		if m.config != nil && m.config.Timeouts != nil {
			host.SetRPCTimeout(m.config.Timeouts.ExtensionRpc())
		}

		// Enterprise required hooks prepended before extension loads
		if m.config != nil && m.config.Enterprise != nil && len(m.config.Enterprise.RequiredHooks) > 0 {
			hooks := make([]struct{ Event, Handler string }, len(m.config.Enterprise.RequiredHooks))
			for i, h := range m.config.Enterprise.RequiredHooks {
				hooks[i] = struct{ Event, Handler string }{Event: h.Event, Handler: h.Handler}
			}
			host.RegisterRequiredHooks(hooks)
		}

		extCfg := &extension.ExtensionConfig{
			ExtensionDir:     filepath.Dir(extPath),
			WorkingDirectory: config.WorkingDirectory,
		}
		if err := host.Load(extPath, extCfg); err != nil {
			utils.Log("Session", "extension load failed for "+extPath+": "+err.Error())
			m.emit(key, types.EngineEvent{
				Type:         "engine_error",
				EventMessage: fmt.Sprintf("extension load failed: %s", err.Error()),
				ErrorCode:    "extension_load_failed",
			})
			continue
		}
		capturedKey := key
		host.SetOnDeath(func(h *extension.Host) {
			m.handleHostDeath(capturedKey, h)
		})
		// Wire async-trigger lifecycle (D-010 / D-011) BEFORE
		// committing any init-time webhook/schedule declarations so
		// the registry's veto pipeline fires through the SDK with a
		// real session context.
		m.wireHostAsync(key, host)
		m.commitHostInitAsyncDecls(key, host)
		// Commit resource declarations (D-007) onto the session broker.
		if errs := host.CommitPendingResourceDecls(s.resourceBroker); len(errs) != 0 {
			for _, err := range errs {
				m.emit(key, types.EngineEvent{
					Type:         "engine_error",
					EventMessage: fmt.Sprintf("resource declaration rejected: %v", err),
					ErrorCode:    "resource_init_rejected",
				})
			}
		}
		group.Add(host)
	}
	if group.IsEmpty() {
		return
	}

	// Wire send_message and persistent emit on each host
	for _, host := range group.Hosts() {
		capturedKey := key
		host.SetOnSendMessage(func(payload extension.SendPromptPayload) {
			// Shared dispatch body (prompt_options.go) so the active-hook path
			// and this fallback path produce identical run configuration.
			// Model + bash-allowlist additions flow through; nothing is dropped.
			go m.dispatchSendPromptPayload(capturedKey, "start_session", payload)
		})
		host.SetPersistentEmit(func(ev types.EngineEvent) {
			if ev.Type == "engine_agent_state" {
				// Cache the extension's roster, then re-emit a merged snapshot
				// that includes engine-managed entries (dispatch state with
				// task, conversationId, progress). Forwarding the extension's
				// raw event would overwrite engine-managed entries on the
				// desktop due to the complete-snapshot contract.
				s.agents.CacheExtStates(ev.Agents)
				merged := s.agents.MergedSnapshot()
				utils.Log("Session", fmt.Sprintf("agent_snapshot_emitted key=%s count=%d reason=ext_emit_merged", capturedKey, len(merged)))
				m.emit(capturedKey, types.EngineEvent{Type: "engine_agent_state", Agents: merged})
				return
			}
			if ev.Type == "engine_status" && ev.Fields != nil && ev.Fields.ExtensionName != "" {
				m.mu.Lock()
				s.extensionName = ev.Fields.ExtensionName
				m.mu.Unlock()
			}
			m.emit(capturedKey, ev)
		})

		// Persistent publish for ext/publish_resource calls from
		// onComplete callbacks (after the run exits, ctxStack is empty).
		// Always publish to session broker first, then fan out to global
		// broker for reliable delivery (per-session subscriptions often
		// fail because the producer only exists on one session's broker).
		host.SetPersistentPublishResource(func(kind string, delta types.ResourceDelta) error {
			if s.resourceBroker != nil {
				if err := s.resourceBroker.Publish(kind, delta); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("no broker available")
			}
			if m.globalBroker != nil {
				m.globalBroker.PublishDirect(kind, delta)
			}
			return nil
		})
	}

	m.mu.Lock()
	s.extGroup = group
	m.mu.Unlock()

	// Fire session_start
	m.emit(key, types.EngineEvent{
		Type:         "engine_working_message",
		EventMessage: "Initializing extensions...",
	})
	ctx := m.newExtContext(s, key)
	_ = group.FireSessionStart(ctx)

	// Start the workspace filesystem watcher after extensions are loaded and
	// session_start has fired. Wiring after session_start lets extensions
	// observe the very first batch of events without a startup-race; the
	// watcher's own startup walk does not synthesize events for pre-existing
	// files, so consumers see only post-start activity.
	if release := m.startWorkspaceWatcher(s, key, group); release != nil {
		m.mu.Lock()
		s.fsWatcherRelease = release
		m.mu.Unlock()
	}

	// Discover capabilities from extensions
	caps := group.FireCapabilityDiscover(ctx)
	for _, cap := range caps {
		for _, host := range group.Hosts() {
			host.SDK().RegisterCapability(cap)
		}
	}

	// Phase 0.5: publish the initial command-registry snapshot, then wire
	// per-host onCommandsChange observers so subsequent mid-session
	// RegisterCommand calls also trigger snapshots.
	//
	// Ordering matters: by emitting the initial snapshot FIRST and wiring
	// observers SECOND, we collapse all init-time RegisterCommand calls
	// (which fire during host.Load() and during FireSessionStart) into a
	// single snapshot event rather than N events with intermediate states.
	// Mid-session registrations after this point each get their own
	// snapshot, which is the desired behavior — a consumer's cached view
	// only needs to be re-warmed for changes that happen after init
	// settles.
	m.emitCommandRegistry(key)
	for _, host := range group.Hosts() {
		capturedKey := key
		host.SetOnCommandsChange(func() {
			m.emitCommandRegistry(capturedKey)
		})
	}
	utils.Log("Session", fmt.Sprintf("loadAndWireExtensions: wired %d onCommandsChange observers for key=%s", len(group.Hosts()), key))
}
