package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dsswift/ion/engine/internal/utils"
)

// sessionBindings is the JSON schema for the key->conversationId sidecar.
// It maps session key strings to their most recently established conversationId.
// Written whenever a session's conversation id is established; read on
// StartSession for resume-on-restart.
type sessionBindings struct {
	Bindings map[string]string `json:"bindings"`
}

// bindingsPath returns the path to the session-bindings sidecar file.
// Honors ION_SESSION_BINDINGS_PATH for test isolation.
func bindingsPath() string {
	if v := os.Getenv("ION_SESSION_BINDINGS_PATH"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ion", "session-bindings.json")
}

// loadBindings reads the binding sidecar from disk. Returns an empty map
// on any error (missing file is not an error -- just means first run).
func loadBindings(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			utils.Log("session-bindings", fmt.Sprintf("load: %v", err))
		}
		return make(map[string]string)
	}
	var sb sessionBindings
	if err := json.Unmarshal(data, &sb); err != nil {
		utils.Log("session-bindings", fmt.Sprintf("unmarshal: %v", err))
		return make(map[string]string)
	}
	if sb.Bindings == nil {
		return make(map[string]string)
	}
	return sb.Bindings
}

// saveBinding atomically persists key->conversationId to the sidecar.
// Best-effort: I/O errors are logged and never fatal.
func saveBinding(path, key, conversationID string) {
	bindings := loadBindings(path)
	bindings[key] = conversationID

	sb := sessionBindings{Bindings: bindings}
	data, err := json.Marshal(sb)
	if err != nil {
		utils.Log("session-bindings", fmt.Sprintf("marshal: %v", err))
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		utils.Log("session-bindings", fmt.Sprintf("mkdir: %v", err))
		return
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		utils.Log("session-bindings", fmt.Sprintf("write tmp: %v", err))
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		utils.Log("session-bindings", fmt.Sprintf("rename: %v", err))
		return
	}
	utils.Log("session-bindings", fmt.Sprintf("saved: key=%s conversationId=%s", key, conversationID))
}

// lookupBinding returns the previously persisted conversationId for the given
// key, or "" if none is stored.
func lookupBinding(path, key string) string {
	bindings := loadBindings(path)
	return bindings[key]
}
