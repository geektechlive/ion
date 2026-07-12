package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"
	"sync"
	"time"
)

const maxTokenStoreEntries = 16

type tokenEntry struct {
	Token     string    `json:"token"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type tokenStoreFile struct {
	Tokens map[string]tokenEntry `json:"tokens"`
}

// tokenStore persists APNs device tokens to disk so they survive in-memory
// channel cleanup (removeIfEmpty) and relay restarts. Writes are atomic
// (temp-file rename, 0600 perms). The store is bounded to
// maxTokenStoreEntries entries; oldest by UpdatedAt is evicted when the
// cap is exceeded.
type tokenStore struct {
	path string
	mu   sync.Mutex
	data tokenStoreFile
}

// newTokenStore loads the store from path. If the file does not exist or is
// corrupt the store starts empty — relay startup must not fail because of a
// missing or damaged token file.
func newTokenStore(path string) *tokenStore {
	s := &tokenStore{
		path: path,
		data: tokenStoreFile{Tokens: make(map[string]tokenEntry)},
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("tokenStore: load %s: %v (starting empty)", path, err)
		}
		return s
	}
	var loaded tokenStoreFile
	if err := json.Unmarshal(raw, &loaded); err != nil {
		log.Printf("tokenStore: parse %s: %v (starting empty)", path, err)
		return s
	}
	if loaded.Tokens != nil {
		s.data = loaded
	}
	return s
}

// get returns the stored APNs token for channelID, or "" if not found.
func (s *tokenStore) get(channelID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Tokens[channelID].Token
}

// set stores the token for channelID, prunes to maxTokenStoreEntries by
// evicting the oldest entry by UpdatedAt, then atomically writes to disk.
func (s *tokenStore) set(channelID, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Tokens[channelID] = tokenEntry{Token: token, UpdatedAt: time.Now()}
	for len(s.data.Tokens) > maxTokenStoreEntries {
		s.evictOldestLocked()
	}
	if err := s.saveLocked(); err != nil {
		log.Printf("tokenStore: save %s: %v", s.path, err)
	}
}

// count returns the number of entries currently in the store (used by tests).
func (s *tokenStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.data.Tokens)
}

// evictOldestLocked removes the entry with the smallest UpdatedAt timestamp.
// Must be called with s.mu held.
func (s *tokenStore) evictOldestLocked() {
	if len(s.data.Tokens) == 0 {
		return
	}
	type kv struct {
		key string
		at  time.Time
	}
	entries := make([]kv, 0, len(s.data.Tokens))
	for k, v := range s.data.Tokens {
		entries = append(entries, kv{k, v.UpdatedAt})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].at.Before(entries[j].at)
	})
	delete(s.data.Tokens, entries[0].key)
}

// saveLocked writes the store to disk atomically via a temp file + rename.
// Must be called with s.mu held.
func (s *tokenStore) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
