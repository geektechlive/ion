package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/http2"
)

const (
	apnsProductionURL = "https://api.push.apple.com"
	apnsSandboxURL    = "https://api.sandbox.push.apple.com"
	apnsTopic         = "com.geektechlive.ion.mobile"
	tokenTTL          = 50 * time.Minute // Apple requires refresh within 60 min

	// defaultRetryBackoff is the pause before the single retry attempt granted
	// to transient failures (network/transport errors and 5xx responses).
	defaultRetryBackoff = 2 * time.Second
)

// pushRequest holds the parameters for a single push notification.
type pushRequest struct {
	deviceToken string
	title       string
	body        string
	kind        string // resource kind for deep-link routing on the client
	resourceId  string // resource ID for deep-link routing on the client
}

// APNsPusher sends push notifications via Apple's HTTP/2 APNs API.
type APNsPusher struct {
	client  *http.Client
	baseURL string
	keyID   string
	teamID  string
	key     *ecdsa.PrivateKey

	// retryBackoff is the pause before the single retry granted to transient
	// failures. Overridable (primarily so tests need not sleep seconds).
	retryBackoff time.Duration

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time

	// Observable send counters. These are additive telemetry: nothing in the
	// send path depends on them, so an unread status surface breaks nothing.
	sentOK           atomic.Int64
	sendFailed       atomic.Int64
	droppedQueueFull atomic.Int64
	skippedNoToken   atomic.Int64

	statsMu       sync.Mutex // guards lastError / lastErrorTime
	lastError     string
	lastErrorTime time.Time

	queue chan pushRequest
}

// PushStats is a point-in-time snapshot of the pusher's observable counters.
// Exposed via the relay's authenticated GET /v1/push/status endpoint so
// Jarvis-side health checks can read APNs delivery health without scraping logs.
type PushStats struct {
	Enabled          bool       `json:"enabled"`
	SentOK           int64      `json:"sent_ok"`
	SendFailed       int64      `json:"send_failed"`
	DroppedQueueFull int64      `json:"dropped_queue_full"`
	SkippedNoToken   int64      `json:"skipped_no_token"`
	LastError        string     `json:"last_error,omitempty"`
	LastErrorTime    *time.Time `json:"last_error_time,omitempty"`
}

// Stats returns a snapshot of the pusher's counters and last recorded error.
func (p *APNsPusher) Stats() PushStats {
	p.statsMu.Lock()
	lastErr := p.lastError
	lastErrTime := p.lastErrorTime
	p.statsMu.Unlock()

	stats := PushStats{
		Enabled:          true,
		SentOK:           p.sentOK.Load(),
		SendFailed:       p.sendFailed.Load(),
		DroppedQueueFull: p.droppedQueueFull.Load(),
		SkippedNoToken:   p.skippedNoToken.Load(),
		LastError:        lastErr,
	}
	if !lastErrTime.IsZero() {
		stats.LastErrorTime = &lastErrTime
	}
	return stats
}

// RecordSkippedNoToken increments the skipped_no_token counter. Called by the
// relay read loop when a push-flagged frame arrives but no APNs token is
// available for the channel (mobile was away and no persisted token existed).
func (p *APNsPusher) RecordSkippedNoToken() {
	p.skippedNoToken.Add(1)
}

// recordFailure increments the failure counter and records the last error for
// the status surface. Callers pass the same stable-prefixed message they log.
func (p *APNsPusher) recordFailure(msg string) {
	p.sendFailed.Add(1)
	p.statsMu.Lock()
	p.lastError = msg
	p.lastErrorTime = time.Now()
	p.statsMu.Unlock()
}

func NewAPNsPusher(keyPath, keyID, teamID string) (*APNsPusher, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read APNs key: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM in APNs key file")
	}

	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse APNs key: %w", err)
	}

	ecKey, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("APNs key is not ECDSA")
	}

	transport := &http2.Transport{}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}

	// Use sandbox for development; production in release builds.
	baseURL := apnsSandboxURL
	if os.Getenv("APNS_PRODUCTION") == "1" {
		baseURL = apnsProductionURL
	}

	return &APNsPusher{
		client:       client,
		baseURL:      baseURL,
		keyID:        keyID,
		teamID:       teamID,
		key:          ecKey,
		retryBackoff: defaultRetryBackoff,
		queue:        make(chan pushRequest, 64),
	}, nil
}

func (p *APNsPusher) getToken() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cachedToken != "" && time.Now().Before(p.tokenExpiry) {
		return p.cachedToken, nil
	}

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": p.teamID,
		"iat": now.Unix(),
	})
	token.Header["kid"] = p.keyID

	signed, err := token.SignedString(p.key)
	if err != nil {
		return "", fmt.Errorf("sign APNs token: %w", err)
	}

	p.cachedToken = signed
	p.tokenExpiry = now.Add(tokenTTL)
	return signed, nil
}

type apnsPayload struct {
	Aps           apsPayload `json:"aps"`
	IonKind       string     `json:"ionKind,omitempty"`
	IonResourceId string     `json:"ionResourceId,omitempty"`
}

type apsPayload struct {
	Alert            apsAlert `json:"alert"`
	Sound            string   `json:"sound,omitempty"`
	Category         string   `json:"category,omitempty"`
	ContentAvailable int      `json:"content-available,omitempty"`
}

type apsAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (p *APNsPusher) Send(deviceToken, title, body, kind, resourceId string) {
	select {
	case p.queue <- pushRequest{deviceToken: deviceToken, title: title, body: body, kind: kind, resourceId: resourceId}:
	default:
		// Backpressure by design: the queue is full, so we drop rather than
		// block the relay read loop. Loud, so operators see sustained overload.
		p.droppedQueueFull.Add(1)
		log.Printf("ERROR: APNs push dropped, queue full")
	}
}

// Start launches a single background worker that drains the push queue.
func (p *APNsPusher) Start() {
	go func() {
		for req := range p.queue {
			p.sendAsync(req)
		}
	}()
}

func (p *APNsPusher) sendAsync(req pushRequest) {
	token, err := p.getToken()
	if err != nil {
		// Terminal: without a signed token no send can succeed.
		msg := fmt.Sprintf("ERROR: APNs token generation failed: %v", err)
		log.Print(msg)
		p.recordFailure(msg)
		return
	}

	payload := apnsPayload{
		Aps: apsPayload{
			Alert: apsAlert{
				Title: req.title,
				Body:  req.body,
			},
			Sound:            "jarvis-message.caf",
			Category:         "PERMISSION_REQUEST",
			ContentAvailable: 1,
		},
		IonKind:       req.kind,
		IonResourceId: req.resourceId,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		// Terminal: a malformed payload will never marshal.
		msg := fmt.Sprintf("ERROR: APNs payload marshal failed: %v", err)
		log.Print(msg)
		p.recordFailure(msg)
		return
	}

	url := fmt.Sprintf("%s/3/device/%s", p.baseURL, req.deviceToken)

	// Attempt once, then grant a single retry to transient failures only.
	// Transport (network) errors and 5xx are transient; 4xx (400/403/410) are
	// terminal and never retried. The first-attempt transient path logs at
	// WARN (no uppercase ERROR/CRITICAL token) so a self-healing blip does not
	// trip the log patrol; only a terminal failure emits an ERROR line.
	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, reason, transient, err := p.attemptSend(url, token, data)
		lastAttempt := attempt == maxAttempts

		if err != nil {
			if transient && !lastAttempt {
				log.Printf("WARN: APNs send transport error, retrying in %s: %v", p.retryBackoff, err)
				time.Sleep(p.retryBackoff)
				continue
			}
			// Log the detailed error, but record a stable, token-free string on
			// the status surface (client.Do's *url.Error embeds the request URL,
			// which contains the device token).
			if transient {
				log.Printf("ERROR: APNs send failed, transport error after retry: %v", err)
				p.recordFailure("ERROR: APNs send failed, transport error after retry")
			} else {
				log.Printf("ERROR: APNs request build failed: %v", err)
				p.recordFailure("ERROR: APNs request build failed")
			}
			return
		}

		if status == http.StatusOK {
			p.sentOK.Add(1)
			return
		}

		// 410 Gone: the device token is no longer valid. Operator-actionable —
		// the stale token should be pruned. Keep the volatile token at the end.
		if status == http.StatusGone {
			log.Printf("ERROR: APNs device token stale (410 Gone) reason=%s deviceToken=%s", reason, req.deviceToken)
			p.recordFailure(fmt.Sprintf("ERROR: APNs device token stale (410 Gone) reason=%s", reason))
			return
		}

		// 5xx: APNs server-side error, transient — retry once.
		if status >= 500 && !lastAttempt {
			log.Printf("WARN: APNs send status=%d (server error), retrying in %s reason=%s", status, p.retryBackoff, reason)
			time.Sleep(p.retryBackoff)
			continue
		}

		// Any other non-200 (terminal 4xx, or 5xx after retry). Stable prefix:
		// status and reason lead; the volatile device token trails.
		log.Printf("ERROR: APNs send failed status=%d reason=%s deviceToken=%s", status, reason, req.deviceToken)
		p.recordFailure(fmt.Sprintf("ERROR: APNs send failed status=%d reason=%s", status, reason))
		return
	}
}

// attemptSend performs a single APNs POST. On a normal HTTP exchange it returns
// the status code and the APNs "reason" string (parsed from the JSON body,
// falling back to the raw body) with err==nil. On failure it returns a non-nil
// err and a transient flag: transient=true for transport-level errors (network,
// connection reset) which the caller may retry; transient=false for a terminal
// request-build failure which must not be retried.
func (p *APNsPusher) attemptSend(url, token string, data []byte) (status int, reason string, transient bool, err error) {
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return 0, "", false, err
	}

	httpReq.Header.Set("Authorization", "bearer "+token)
	httpReq.Header.Set("apns-topic", apnsTopic)
	httpReq.Header.Set("apns-push-type", "alert")
	httpReq.Header.Set("apns-priority", "10")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return 0, "", true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, "", false, nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, parseAPNsReason(respBody), false, nil
}

// parseAPNsReason extracts the "reason" field from an APNs error body
// (e.g. {"reason":"BadDeviceToken"}), falling back to the trimmed raw body.
func parseAPNsReason(body []byte) string {
	var parsed struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Reason != "" {
		return parsed.Reason
	}
	return string(bytes.TrimSpace(body))
}
