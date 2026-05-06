package session

import (
	"github.com/dsswift/ion/engine/internal/backend"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// AbortAgent sends SIGTERM to the named agent process. If subtree is true,
// it walks the parentAgent chain to find all descendant agents and aborts them.
//
// Special case: if agentName is empty and subtree is true, every agent in
// the session is aborted. The user-facing interrupt button uses this when
// the parent run is already dead but dispatched children are still alive.
func (m *Manager) AbortAgent(key, agentName string, subtree bool) {
	if agentName == "" && subtree {
		m.abortAllDescendants(key, "user abort (all)")
		return
	}

	m.mu.RLock()
	s, ok := m.sessions[key]
	if !ok {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	var pidsToKill []int

	if subtree {
		all := s.agents.AllHandles()
		for name, handle := range all {
			if name == agentName || s.agents.IsDescendant(name, agentName) {
				pidsToKill = append(pidsToKill, handle.PID)
			}
		}
	} else {
		if handle, exists := s.agents.LookupHandle(agentName); exists {
			pidsToKill = append(pidsToKill, handle.PID)
		}
	}

	for _, pid := range pidsToKill {
		killProcess(pid)
	}
}

// SteerAgent sends a message to a running agent's stdin, or steers the main
// session loop if agentName is empty.
func (m *Manager) SteerAgent(key, agentName, message string) {
	m.mu.RLock()
	s, ok := m.sessions[key]
	if !ok {
		m.mu.RUnlock()
		return
	}

	// If agentName is empty, steer the main session loop
	if agentName == "" {
		rid := s.requestID
		m.mu.RUnlock()
		if rid != "" {
			if apiBackend, ok := m.backend.(*backend.ApiBackend); ok {
				apiBackend.Steer(rid, message)
			} else {
				// CliBackend: write follow-up message over stdin pipe
				stdinMsg := map[string]interface{}{
					"type": "user",
					"message": map[string]interface{}{
						"role": "user",
						"content": []map[string]interface{}{
							{"type": "text", "text": message},
						},
					},
				}
				if err := m.backend.WriteToStdin(rid, stdinMsg); err != nil {
					utils.Log("Session", "steer via stdin failed: "+err.Error())
				}
			}
		}
		return
	}
	m.mu.RUnlock()

	handle, exists := s.agents.LookupHandle(agentName)
	if !exists {
		return
	}
	if handle.StdinWrite != nil {
		handle.StdinWrite(message)
	}
}

// resolveAgentSpec resolves an agent name to a registered spec. If the name
// is not in the session's spec registry, fires the capability_match hook so
// extensions can promote a draft (typically via ctx.RegisterAgentSpec) and
// retries resolution on the same call. Returns (spec, true) on success, or
// (zero, false) when no match is registered after the hook runs.
func (m *Manager) resolveAgentSpec(s *engineSession, key, name string) (types.AgentSpec, bool) {
	if spec, ok := s.agents.LookupSpec(name); ok {
		return spec, true
	}

	if s.extGroup == nil {
		return types.AgentSpec{}, false
	}

	known := s.agents.AllSpecNames()

	extCtx := m.newExtContext(s, key)
	for _, h := range s.extGroup.Hosts() {
		_ = h.SDK().FireCapabilityMatch(extCtx, extension.CapabilityMatchInfo{
			Input:        name,
			Capabilities: known,
		})
	}

	// Retry — handler may have called ctx.RegisterAgentSpec.
	return s.agents.LookupSpec(name)
}
