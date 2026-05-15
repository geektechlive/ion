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
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/net/http2"
)

const (
	apnsProductionURL = "https://api.push.apple.com"
	apnsSandboxURL    = "https://api.sandbox.push.apple.com"
	apnsTopic         = "com.geektechlive.ion.mobile"
	tokenTTL          = 50 * time.Minute // Apple requires refresh within 60 min
)

// pushRequest holds the parameters for a single push notification.
type pushRequest struct {
	deviceToken  string
	title        string
	body         string
	sound        string
	briefingID   string
	briefingText string
}

// APNsPusher sends push notifications via Apple's HTTP/2 APNs API.
type APNsPusher struct {
	client  *http.Client
	baseURL string
	keyID   string
	teamID  string
	key     *ecdsa.PrivateKey

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time

	queue chan pushRequest
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
		client:  client,
		baseURL: baseURL,
		keyID:   keyID,
		teamID:  teamID,
		key:     ecKey,
		queue:   make(chan pushRequest, 64),
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

// apnsBriefingTextLimit is the max byte length for briefingText embedded in
// the APNs payload. APNs caps alert payloads at 4 KB; this leaves margin for
// aps + title + body + briefingId. If briefingText exceeds this limit it is
// truncated and the iOS app falls back to the drain-on-reconnect path for the
// full version.
const apnsBriefingTextLimit = 2800

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

// truncateBriefingText cuts briefingText to fit under the APNs payload cap
// and appends a marker so the iOS app can hint the user that more is available.
func truncateBriefingText(s string) string {
	if len(s) <= apnsBriefingTextLimit {
		return s
	}
	const marker = "\n…[truncated — open Briefings for full text]"
	return s[:apnsBriefingTextLimit-len(marker)] + marker
}

func (p *APNsPusher) Send(deviceToken, title, body, sound, briefingID, briefingText string) {
	select {
	case p.queue <- pushRequest{
		deviceToken:  deviceToken,
		title:        title,
		body:         body,
		sound:        sound,
		briefingID:   briefingID,
		briefingText: briefingText,
	}:
	default:
		log.Printf("APNs push queue full, dropping notification")
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
		log.Printf("APNs token error: %v", err)
		return
	}

	sound := req.sound
	if sound == "" {
		sound = "jarvis-message.caf"
	}

	// Top-level keys: aps + arbitrary custom data (briefingId, briefingText)
	// per APNs spec. Marshal as a map so we can omit empties cleanly.
	payload := map[string]any{
		"aps": apsPayload{
			Alert: apsAlert{
				Title: req.title,
				Body:  req.body,
			},
			Sound:            sound,
			Category:         "PERMISSION_REQUEST",
			ContentAvailable: 1,
		},
	}
	if req.briefingID != "" {
		payload["briefingId"] = req.briefingID
	}
	if req.briefingText != "" {
		payload["briefingText"] = truncateBriefingText(req.briefingText)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("APNs marshal error: %v", err)
		return
	}

	url := fmt.Sprintf("%s/3/device/%s", p.baseURL, req.deviceToken)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		log.Printf("APNs request error: %v", err)
		return
	}

	httpReq.Header.Set("Authorization", "bearer "+token)
	httpReq.Header.Set("apns-topic", apnsTopic)
	httpReq.Header.Set("apns-push-type", "alert")
	httpReq.Header.Set("apns-priority", "10")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Printf("APNs send error: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("APNs response %d: %s", resp.StatusCode, string(respBody))
	}
}
