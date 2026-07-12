package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// startTestRelayFull starts a relay server backed by the provided hub and
// pusher. Unlike startTestRelay (which creates its own hub with nil pusher),
// this helper accepts a pre-configured hub so tests can inject a token store
// and a real pusher to exercise the full persistence + push path.
func startTestRelayFull(t *testing.T, apiKey string, hub *Hub, pusher *APNsPusher) *httptest.Server {
	t.Helper()
	auth := NewAuthMiddleware(apiKey)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/channel/{channelId}", func(w http.ResponseWriter, r *http.Request) {
		if !auth.Validate(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		channelID := r.PathValue("channelId")
		role := r.URL.Query().Get("role")
		if role != "ion" && role != "mobile" {
			http.Error(w, "role must be 'ion' or 'mobile'", http.StatusBadRequest)
			return
		}
		hub.HandleWebSocket(w, r, channelID, role, pusher)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.CloseAll()
		server.Close()
	})
	return server
}

// TestAPNsTokenRestoredAfterChannelCleanup verifies that when mobile has
// connected and supplied a token, the channel is subsequently emptied by
// removeIfEmpty (e.g. desktop restart while phone is away), and a new ion
// connection arrives, the token is restored from the persistent store and
// the push-flagged message proceeds to the APNs server (sentOK == 1).
//
// Regression guard: revering the getOrCreateChannel restore logic causes
// apnsToken to be "" on the new channel, skippedNoToken increments instead,
// and sentOK stays 0 — the test goes red.
func TestAPNsTokenRestoredAfterChannelCleanup(t *testing.T) {
	const apiKey = "test-key-restore"
	const channelID = "chan-restore"
	const deviceToken = "device-token-restore-abc"

	// Fake APNs server that returns 200 OK.
	apnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer apnsSrv.Close()

	pusher := newTestPusher(t, apnsSrv.Client(), apnsSrv.URL)
	pusher.Start()

	hub := NewHub()
	hub.tokenStore = newTokenStore(filepath.Join(t.TempDir(), "tokens.json"))

	// Seed the token as if mobile had connected and later disconnected, causing
	// the channel to be cleaned up by removeIfEmpty.
	hub.tokenStore.set(channelID, deviceToken)

	server := startTestRelayFull(t, apiKey, hub, pusher)

	// Connect only ion — mobile is absent, no active channel for this id yet.
	ionConn := dialWS(t, server, channelID, "ion", apiKey)

	// Send a push-flagged message. The relay read loop should restore the
	// persisted token from the store and forward to APNs.
	ctx := context.Background()
	msg := `{"push":true,"pushTitle":"Test","pushBody":"Body"}`
	if err := ionConn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write push message: %v", err)
	}

	// pusher.Send enqueues to the queue; Start() drains async. Poll with a
	// deadline matching the pattern used in relay_test.go.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pusher.sentOK.Load() == 1 {
			return // pass
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("expected sentOK=1 after token restore from store, got sentOK=%d skippedNoToken=%d",
		pusher.sentOK.Load(), pusher.skippedNoToken.Load())
}

// TestAPNsTokenRestoredAfterRelayRestart simulates a relay restart: a first
// hub instance captures the token into a shared store file, then a second
// hub instance (fresh in-memory state) reads from the same file. When
// getOrCreateChannel is called on the second hub, the token must be present.
func TestAPNsTokenRestoredAfterRelayRestart(t *testing.T) {
	const channelID = "chan-restart"
	const deviceToken = "device-token-restart-xyz"

	storePath := filepath.Join(t.TempDir(), "tokens.json")

	// First relay instance: captures token and persists it.
	store1 := newTokenStore(storePath)
	store1.set(channelID, deviceToken)

	// Second relay instance: fresh in-memory state, same on-disk file.
	hub2 := NewHub()
	hub2.tokenStore = newTokenStore(storePath)

	ch := hub2.getOrCreateChannel(channelID)
	ch.mu.Lock()
	restored := ch.apnsToken
	ch.mu.Unlock()

	if restored != deviceToken {
		t.Errorf("expected token %q restored after relay restart, got %q", deviceToken, restored)
	}
}

// TestPushSkippedNoTokenIncrements verifies that when a push-flagged frame
// arrives from ion with no APNs token anywhere (no mobile connection, no
// persisted token), skippedNoToken increments and no panic occurs. The APNs
// server must never be called.
func TestPushSkippedNoTokenIncrements(t *testing.T) {
	buf := captureLogs(t)

	const apiKey = "test-key-skip"
	const channelID = "chan-skip-notoken"

	// APNs server that must not be called.
	apnsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("APNs server must not be called when no token is available")
		w.WriteHeader(http.StatusOK)
	}))
	defer apnsSrv.Close()

	pusher := newTestPusher(t, apnsSrv.Client(), apnsSrv.URL)
	pusher.Start()

	// Hub with no token store — no persisted token for this channel.
	hub := NewHub()

	server := startTestRelayFull(t, apiKey, hub, pusher)

	// Connect only ion (no mobile, no token anywhere).
	ionConn := dialWS(t, server, channelID, "ion", apiKey)

	// Send a push-flagged message.
	ctx := context.Background()
	msg := `{"push":true,"pushTitle":"T","pushBody":"B"}`
	if err := ionConn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatalf("write push message: %v", err)
	}

	// skippedNoToken increments synchronously in the relay read loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pusher.skippedNoToken.Load() == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pusher.skippedNoToken.Load() != 1 {
		t.Errorf("expected skippedNoToken=1, got sentOK=%d skippedNoToken=%d",
			pusher.sentOK.Load(), pusher.skippedNoToken.Load())
	}
	// Part 2 spec: the ERROR log line must appear.
	if !strings.Contains(buf.String(), "ERROR: push skipped: no APNs token channel="+channelID) {
		t.Errorf("expected ERROR log line for skipped push, got: %s", buf.String())
	}
}

// TestAPNsTokenCapturedOnMobileConnect verifies that when a mobile peer
// connects with an apns_token query parameter, the token is written through to
// the persistent store. This pins the HandleWebSocket capture path so that
// revering the h.tokenStore.set call causes the test to fail.
func TestAPNsTokenCapturedOnMobileConnect(t *testing.T) {
	const apiKey = "test-key-capture"
	const channelID = "chan-capture"
	const deviceToken = "captured-device-token-xyz"

	hub := NewHub()
	hub.tokenStore = newTokenStore(filepath.Join(t.TempDir(), "tokens.json"))

	server := startTestRelayFull(t, apiKey, hub, nil)

	// Dial as mobile with the APNs token in the query string, as the iOS app does
	// on initial connection.
	rawURL := fmt.Sprintf("ws%s/v1/channel/%s?role=mobile&apns_token=%s",
		strings.TrimPrefix(server.URL, "http"), channelID, deviceToken)
	conn, _, err := websocket.Dial(context.Background(), rawURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + apiKey}},
	})
	if err != nil {
		t.Fatalf("dial mobile with apns_token: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	// Poll until the relay handler processes the connection and writes through.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.tokenStore.get(channelID) == deviceToken {
			return // pass
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("expected token %q written through to store on mobile connect, got %q",
		deviceToken, hub.tokenStore.get(channelID))
}

// TestTokenStoreBounding verifies that the store never exceeds
// maxTokenStoreEntries entries, and that the bound is respected both in-memory
// and after reloading from disk.
func TestTokenStoreBounding(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "tokens.json")
	store := newTokenStore(storePath)

	// Insert maxTokenStoreEntries + 4 entries.
	const extra = 4
	for i := 0; i < maxTokenStoreEntries+extra; i++ {
		store.set(fmt.Sprintf("chan%d", i), fmt.Sprintf("token%d", i))
	}

	if got := store.count(); got > maxTokenStoreEntries {
		t.Errorf("in-memory store exceeded cap: %d > %d", got, maxTokenStoreEntries)
	}

	// Reload from disk to verify the bound is honoured in the persisted file.
	store2 := newTokenStore(storePath)
	if got := store2.count(); got > maxTokenStoreEntries {
		t.Errorf("persisted store exceeded cap: %d > %d", got, maxTokenStoreEntries)
	}
}
