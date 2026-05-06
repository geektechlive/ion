// Package pending manages typed pending-request maps with register/resolve
// semantics. Thread-safe with its own mutex — does not import session.
package pending

import (
	"sync"
)

// ElicitReply carries a client's response to an engine_elicitation_request event.
type ElicitReply struct {
	Response  map[string]interface{}
	Cancelled bool
}

// Broker manages pending permission, dialog, and elicitation request maps.
// Each map type uses a buffered channel so callers can block until the
// response arrives.
type Broker struct {
	mu          sync.RWMutex
	permissions map[string]chan string
	dialogs     map[string]chan interface{}
	elicits     map[string]chan ElicitReply
}

// New creates a ready-to-use Broker.
func New() *Broker {
	return &Broker{
		permissions: make(map[string]chan string),
		dialogs:     make(map[string]chan interface{}),
		elicits:     make(map[string]chan ElicitReply),
	}
}

// --- Permissions ---

// RegisterPermission creates a channel for an in-flight permission request.
// Returns the channel the hook server should block on.
func (b *Broker) RegisterPermission(id string) chan string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan string, 1)
	b.permissions[id] = ch
	return ch
}

// ResolvePermission sends the chosen option ID to the waiting channel.
// Non-blocking: if nobody is listening the send is dropped.
func (b *Broker) ResolvePermission(id string, optionID string) {
	b.mu.RLock()
	ch, ok := b.permissions[id]
	b.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case ch <- optionID:
	default:
	}
}

// UnregisterPermission removes a pending permission entry.
func (b *Broker) UnregisterPermission(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.permissions, id)
}

// --- Dialogs ---

// RegisterDialog creates a channel for an in-flight dialog request.
func (b *Broker) RegisterDialog(id string) chan interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan interface{}, 1)
	b.dialogs[id] = ch
	return ch
}

// ResolveDialog sends the value to the waiting dialog channel.
// Non-blocking: if nobody is listening the send is dropped.
func (b *Broker) ResolveDialog(id string, value interface{}) bool {
	b.mu.RLock()
	ch, ok := b.dialogs[id]
	b.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case ch <- value:
	default:
	}
	return true
}

// UnregisterDialog removes a pending dialog entry.
func (b *Broker) UnregisterDialog(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.dialogs, id)
}

// --- Elicitations ---

// RegisterElicit creates a channel for an in-flight elicitation request.
func (b *Broker) RegisterElicit(id string) chan ElicitReply {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan ElicitReply, 1)
	b.elicits[id] = ch
	return ch
}

// ResolveElicit sends the reply to the waiting elicitation channel.
// Non-blocking: if nobody is listening the send is dropped.
func (b *Broker) ResolveElicit(id string, reply ElicitReply) bool {
	b.mu.RLock()
	ch, ok := b.elicits[id]
	b.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case ch <- reply:
	default:
	}
	return true
}

// UnregisterElicit removes a pending elicitation entry.
func (b *Broker) UnregisterElicit(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.elicits, id)
}
