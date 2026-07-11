package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// errFakeTransport is a synthetic transport-level error used to exercise the
// retry path deterministically via a fake RoundTripper.
var errFakeTransport = errors.New("fake transport failure")

// newTestPusher builds an APNsPusher wired to the given HTTP client + base URL,
// with a freshly generated P-256 signing key so getToken() succeeds without a
// key file. retryBackoff is set tiny so retry tests do not sleep for seconds.
func newTestPusher(t *testing.T, client *http.Client, baseURL string) *APNsPusher {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return &APNsPusher{
		client:       client,
		baseURL:      baseURL,
		keyID:        "TESTKEYID",
		teamID:       "TESTTEAMID",
		key:          key,
		retryBackoff: 1 * time.Millisecond,
		queue:        make(chan pushRequest, 4),
	}
}

// captureLogs redirects the standard logger to a buffer for the duration of the
// test and returns the buffer.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// countingRoundTripper fails the first N calls with a transport error, then
// serves 200 OK. Fully deterministic — no hijack races.
type countingRoundTripper struct {
	calls     atomic.Int64
	failFirst int64
	err       error
}

func (rt *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	n := rt.calls.Add(1)
	if n <= rt.failFirst {
		return nil, rt.err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestRetryOnceOnRequestError(t *testing.T) {
	captureLogs(t)
	rt := &countingRoundTripper{failFirst: 1, err: errFakeTransport}
	p := newTestPusher(t, &http.Client{Transport: rt}, "https://apns.example")

	p.sendAsync(pushRequest{deviceToken: "abc", title: "T", body: "B"})

	if got := rt.calls.Load(); got != 2 {
		t.Errorf("expected 2 transport attempts (1 fail + 1 retry), got %d", got)
	}
	if got := p.sentOK.Load(); got != 1 {
		t.Errorf("expected sent_ok=1 after successful retry, got %d", got)
	}
	if got := p.sendFailed.Load(); got != 0 {
		t.Errorf("expected send_failed=0 after recovery, got %d", got)
	}
}

func TestNoRetryOn410(t *testing.T) {
	buf := captureLogs(t)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	defer srv.Close()

	p := newTestPusher(t, srv.Client(), srv.URL)
	p.sendAsync(pushRequest{deviceToken: "stale-token", title: "T", body: "B"})

	if got := hits.Load(); got != 1 {
		t.Errorf("410 must not be retried: expected 1 hit, got %d", got)
	}
	if got := p.sendFailed.Load(); got != 1 {
		t.Errorf("expected send_failed=1, got %d", got)
	}
	if got := p.sentOK.Load(); got != 0 {
		t.Errorf("expected sent_ok=0, got %d", got)
	}
	logs := buf.String()
	if !strings.Contains(logs, "ERROR: APNs device token stale (410 Gone)") {
		t.Errorf("missing stable 410 ERROR prefix in logs: %q", logs)
	}
	if !strings.Contains(logs, "reason=Unregistered") {
		t.Errorf("expected parsed reason in log, got: %q", logs)
	}
	// Stats surface reflects the failure.
	st := p.Stats()
	if !st.Enabled || st.SendFailed != 1 || st.LastError == "" || st.LastErrorTime == nil {
		t.Errorf("stats not populated after 410: %+v", st)
	}
}

func TestQueueFullDropsAndLogs(t *testing.T) {
	buf := captureLogs(t)
	// Pusher with a zero-length queue-drain (never Start()ed). Fill to capacity,
	// then one more Send must hit the default drop branch.
	p := newTestPusher(t, &http.Client{}, "https://apns.example")
	for i := 0; i < cap(p.queue); i++ {
		p.Send("tok", "T", "B", "", "")
	}
	// Queue is now full; this Send must drop.
	p.Send("tok", "T", "B", "", "")

	if got := p.droppedQueueFull.Load(); got != 1 {
		t.Errorf("expected dropped_queue_full=1, got %d", got)
	}
	logs := buf.String()
	const wantPrefix = "ERROR: APNs push dropped, queue full"
	if !strings.HasPrefix(strings.TrimSpace(logs), wantPrefix) {
		t.Errorf("queue-full log missing stable prefix %q, got: %q", wantPrefix, logs)
	}
}

func TestCountersAccurateAcrossMixedOutcomes(t *testing.T) {
	captureLogs(t)
	var mode atomic.Int64 // 0 => 200, 1 => 400 terminal
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode.Load() == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"reason":"BadDeviceToken"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestPusher(t, srv.Client(), srv.URL)

	// 3 successful sends.
	for i := 0; i < 3; i++ {
		p.sendAsync(pushRequest{deviceToken: "ok", title: "T", body: "B"})
	}
	// 2 terminal 400 failures.
	mode.Store(1)
	for i := 0; i < 2; i++ {
		p.sendAsync(pushRequest{deviceToken: "bad", title: "T", body: "B"})
	}

	if got := p.sentOK.Load(); got != 3 {
		t.Errorf("expected sent_ok=3, got %d", got)
	}
	if got := p.sendFailed.Load(); got != 2 {
		t.Errorf("expected send_failed=2, got %d", got)
	}
	st := p.Stats()
	if st.SentOK != 3 || st.SendFailed != 2 {
		t.Errorf("Stats snapshot wrong: %+v", st)
	}
	if !strings.Contains(st.LastError, "status=400") || !strings.Contains(st.LastError, "reason=BadDeviceToken") {
		t.Errorf("last_error should carry stable status+reason, got %q", st.LastError)
	}
}

func TestTerminalTransportErrorOmitsTokenFromStatus(t *testing.T) {
	captureLogs(t)
	// Fail both attempts so the terminal transport-error path is exercised.
	rt := &countingRoundTripper{failFirst: 2, err: errFakeTransport}
	p := newTestPusher(t, &http.Client{Transport: rt}, "https://apns.example")

	const secretToken = "SUPERSECRETDEVICETOKEN"
	p.sendAsync(pushRequest{deviceToken: secretToken, title: "T", body: "B"})

	if got := rt.calls.Load(); got != 2 {
		t.Errorf("expected 2 attempts before terminal failure, got %d", got)
	}
	if got := p.sendFailed.Load(); got != 1 {
		t.Errorf("expected send_failed=1, got %d", got)
	}
	// The status surface must not leak the device token.
	st := p.Stats()
	if strings.Contains(st.LastError, secretToken) {
		t.Errorf("device token leaked into status last_error: %q", st.LastError)
	}
	if st.LastError == "" {
		t.Error("expected a recorded last_error after terminal transport failure")
	}
}

func TestRetryThenTerminalOn5xx(t *testing.T) {
	captureLogs(t)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable) // 503, always transient
	}))
	defer srv.Close()

	p := newTestPusher(t, srv.Client(), srv.URL)
	p.sendAsync(pushRequest{deviceToken: "tok", title: "T", body: "B"})

	if got := hits.Load(); got != 2 {
		t.Errorf("5xx should be retried exactly once: expected 2 hits, got %d", got)
	}
	if got := p.sendFailed.Load(); got != 1 {
		t.Errorf("expected send_failed=1 after retry exhausted, got %d", got)
	}
}
