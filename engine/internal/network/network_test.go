package network

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
)

// resetGlobals resets package-level state between tests.
func resetGlobals() {
	httpTransport = nil
	proxyURL = nil
	noProxyList = nil
}

// writeTempCACert generates a self-signed CA cert, writes it to a temp file,
// and returns the path. The caller is responsible for removing the file.
func writeTempCACert(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	f, err := os.CreateTemp("", "ion-test-ca-*.pem")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer f.Close()

	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encode pem: %v", err)
	}

	return f.Name()
}

// boolPtr returns a pointer to the given bool, for use in struct literals.
func boolPtr(b bool) *bool { return &b }

// --- IsNoProxy -----------------------------------------------------------------

func TestIsNoProxy(t *testing.T) {
	tests := []struct {
		name     string
		list     []string
		host     string
		expected bool
	}{
		{"exact match", []string{"localhost"}, "localhost", true},
		{"no match", []string{"localhost"}, "example.com", false},
		{"wildcard", []string{"*"}, "anything.example.com", true},
		{"dot prefix match", []string{".example.com"}, "api.example.com", true},
		{"dot prefix no match on root", []string{".example.com"}, "example.com", false},
		{"suffix without dot", []string{"example.com"}, "api.example.com", true},
		{"case insensitive", []string{"LOCALHOST"}, "localhost", true},
		{"empty list", nil, "localhost", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noProxyList = tt.list
			got := IsNoProxy(tt.host)
			if got != tt.expected {
				t.Errorf("IsNoProxy(%q) = %v, want %v", tt.host, got, tt.expected)
			}
		})
	}
}

// --- InitNetwork / GetHTTPTransport --------------------------------------------

func TestInitNetworkDefaults(t *testing.T) {
	resetGlobals()

	InitNetwork(nil)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
}

func TestInitNetworkWithProxy(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpsProxy: "http://proxy.example.com:8080",
			NoProxy:    "localhost,127.0.0.1,.internal.net",
		},
	}

	InitNetwork(cfg)

	proxyFor := GetProxyForURL("https://api.example.com")
	if proxyFor != "http://proxy.example.com:8080" {
		t.Errorf("expected proxy URL, got %q", proxyFor)
	}

	proxyFor = GetProxyForURL("https://localhost:3000")
	if proxyFor != "" {
		t.Errorf("expected empty proxy for localhost, got %q", proxyFor)
	}

	proxyFor = GetProxyForURL("https://foo.internal.net/api")
	if proxyFor != "" {
		t.Errorf("expected empty proxy for .internal.net, got %q", proxyFor)
	}
}

// TestInitNetworkProxyTransport confirms that the HTTP transport returned after
// InitNetwork will route a request through the configured proxy.
func TestInitNetworkProxyTransport(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpsProxy: "http://proxy.example.com:3128",
		},
	}
	InitNetwork(cfg)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.Proxy == nil {
		t.Fatal("expected transport.Proxy to be set")
	}
}

// TestInitNetworkCustomCACert verifies that a valid PEM cert path is loaded
// into the transport's TLS RootCAs pool.
func TestInitNetworkCustomCACert(t *testing.T) {
	resetGlobals()

	certPath := writeTempCACert(t)
	defer os.Remove(certPath)

	cfg := &types.NetworkConfig{
		CustomCaCerts: []string{certPath},
	}
	InitNetwork(cfg)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be set")
	}
	if transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("expected RootCAs pool to be populated")
	}
}

// TestInitNetworkCustomCACertMissingFile verifies that a non-existent cert path
// is silently skipped and InitNetwork still produces a usable transport.
func TestInitNetworkCustomCACertMissingFile(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		CustomCaCerts: []string{"/nonexistent/path/ca.pem"},
	}
	InitNetwork(cfg)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport even with bad cert path")
	}
}

// TestInitNetworkTLSSkipVerify verifies InsecureSkipVerify is set when
// RejectUnauthorized is explicitly false.
func TestInitNetworkTLSSkipVerify(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		RejectUnauthorized: boolPtr(false),
	}
	InitNetwork(cfg)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be set")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify = true")
	}
}

// TestInitNetworkTLSRejectAuthorized verifies InsecureSkipVerify is false when
// RejectUnauthorized is true (the secure default).
func TestInitNetworkTLSRejectAuthorized(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		RejectUnauthorized: boolPtr(true),
	}
	InitNetwork(cfg)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.TLSClientConfig == nil {
		t.Fatal("expected TLSClientConfig to be set")
	}
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify = false when RejectUnauthorized = true")
	}
}

// TestGetHTTPTransportBeforeInit verifies that calling GetHTTPTransport before
// InitNetwork returns a non-nil fallback transport.
func TestGetHTTPTransportBeforeInit(t *testing.T) {
	resetGlobals()

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil default transport before InitNetwork")
	}
}

// TestGetHTTPTransportAfterInit verifies the transport returned after InitNetwork
// is the one that was configured (not the default clone).
func TestGetHTTPTransportAfterInit(t *testing.T) {
	resetGlobals()

	InitNetwork(nil)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	// The returned pointer must be the same object stored in httpTransport.
	if transport != httpTransport {
		t.Error("GetHTTPTransport should return the configured httpTransport instance")
	}
}

// TestInitNetworkTransportSettings verifies the configured transport has TCP
// keepalive, timeouts, and — critically — HTTP/2 DISABLED. The transport is
// pinned to HTTP/1.1 (ForceAttemptHTTP2:false + a non-nil empty TLSNextProto)
// because ResponseHeaderTimeout, which bounds the wait for the first response
// byte, is honored only by Go's HTTP/1.1 transport and never by its HTTP/2
// transport. See internal/network/network.go for the full rationale.
func TestInitNetworkTransportSettings(t *testing.T) {
	resetGlobals()

	InitNetwork(nil)

	transport := GetHTTPTransport()
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	if transport.TLSHandshakeTimeout != 10*time.Second {
		t.Errorf("TLSHandshakeTimeout = %v, want 10s", transport.TLSHandshakeTimeout)
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be false so ResponseHeaderTimeout is honored")
	}
	// Non-nil empty TLSNextProto is the canonical guarantee that the transport
	// never negotiates HTTP/2 over ALPN. A nil map would let the transport
	// upgrade to h2, which would silently neuter ResponseHeaderTimeout.
	if transport.TLSNextProto == nil {
		t.Error("TLSNextProto must be a non-nil (empty) map to guarantee HTTP/1.1")
	}
	if len(transport.TLSNextProto) != 0 {
		t.Errorf("TLSNextProto should be empty, got %d entries", len(transport.TLSNextProto))
	}
	if transport.MaxIdleConns != 100 {
		t.Errorf("MaxIdleConns = %d, want 100", transport.MaxIdleConns)
	}
	if transport.IdleConnTimeout != 90*time.Second {
		t.Errorf("IdleConnTimeout = %v, want 90s", transport.IdleConnTimeout)
	}
	if transport.ExpectContinueTimeout != 1*time.Second {
		t.Errorf("ExpectContinueTimeout = %v, want 1s", transport.ExpectContinueTimeout)
	}
	if transport.DialContext == nil {
		t.Error("DialContext should be set (for dial timeout and TCP keepalive)")
	}
	if transport.ResponseHeaderTimeout != 60*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 60s (caps first-byte wait for long LLM streams)", transport.ResponseHeaderTimeout)
	}
	// HTTP/2 must be off, so the h2-only PING config must be absent.
	if transport.HTTP2 != nil {
		t.Error("HTTP2 config must be nil now that the transport is pinned to HTTP/1.1")
	}
}

// TestResponseHeaderTimeoutFiresOnSilentServer proves the fix end to end: a TCP
// server that ACCEPTS the connection but never writes response headers must
// cause client.Do() to fail with a "timeout awaiting response headers" error,
// rather than blocking forever. Under the old ForceAttemptHTTP2:true config the
// ResponseHeaderTimeout was inert; on HTTP/1.1 it fires. The transport under
// test is the real configured transport (proving its HTTP/1.1 pinning), cloned
// only to shorten ResponseHeaderTimeout so the test is fast (~200ms, not 60s).
func TestResponseHeaderTimeoutFiresOnSilentServer(t *testing.T) {
	resetGlobals()
	InitNetwork(nil)

	// Listener that accepts connections and then goes silent — never writing a
	// response. This reproduces the OpenRouter "headers never arrive" hang.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var connMu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed on teardown
			}
			// Hold the connection open but never respond. Track it (guarded)
			// so teardown can close it.
			connMu.Lock()
			conns = append(conns, conn)
			connMu.Unlock()
		}
	}()
	defer func() {
		_ = ln.Close() // unblocks Accept so the goroutine returns
		connMu.Lock()
		for _, conn := range conns {
			_ = conn.Close()
		}
		connMu.Unlock()
	}()

	// Clone the real configured transport and shorten only the header timeout so
	// the assertion is fast. Everything else (HTTP/1.1 pinning) is unchanged.
	base := GetHTTPTransport()
	tr := base.Clone()
	tr.ResponseHeaderTimeout = 200 * time.Millisecond
	if tr.ForceAttemptHTTP2 {
		t.Fatal("cloned transport unexpectedly has ForceAttemptHTTP2=true")
	}

	client := &http.Client{Transport: tr}

	done := make(chan error, 1)
	go func() {
		//nolint:noctx // the transport-owned ResponseHeaderTimeout is the timer under test
		resp, err := client.Get("http://" + ln.Addr().String())
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error, got nil")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "timeout") {
			t.Errorf("expected a timeout error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client.Do did not return within 5s — ResponseHeaderTimeout did not fire (HTTP/2 not disabled?)")
	}
}

// TestInitNetworkOverwritesPreviousConfig verifies that calling InitNetwork a
// second time replaces the previously configured transport and proxy.
func TestInitNetworkOverwritesPreviousConfig(t *testing.T) {
	resetGlobals()

	first := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpsProxy: "http://first-proxy.example.com:8080",
		},
	}
	InitNetwork(first)

	firstTransport := GetHTTPTransport()
	firstProxy := GetProxyForURL("https://api.example.com")
	if firstProxy != "http://first-proxy.example.com:8080" {
		t.Fatalf("first InitNetwork: expected first proxy, got %q", firstProxy)
	}

	// Call InitNetwork again with a different config.
	second := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpsProxy: "http://second-proxy.example.com:9090",
		},
	}
	InitNetwork(second)

	secondTransport := GetHTTPTransport()
	secondProxy := GetProxyForURL("https://api.example.com")

	if secondProxy != "http://second-proxy.example.com:9090" {
		t.Errorf("second InitNetwork: expected second proxy, got %q", secondProxy)
	}
	if firstTransport == secondTransport {
		t.Error("second InitNetwork should produce a new transport instance")
	}
}

// TestInitNetworkHttpFallback verifies that HttpProxy is used when HttpsProxy
// is not set.
func TestInitNetworkHttpFallback(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpProxy: "http://http-only-proxy.example.com:8080",
		},
	}
	InitNetwork(cfg)

	got := GetProxyForURL("https://api.example.com")
	if got != "http://http-only-proxy.example.com:8080" {
		t.Errorf("expected http-only proxy fallback, got %q", got)
	}
}

// TestInitNetworkHttpsProxyTakesPrecedence verifies HttpsProxy wins over HttpProxy.
func TestInitNetworkHttpsProxyTakesPrecedence(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpProxy:  "http://http-proxy.example.com:8080",
			HttpsProxy: "http://https-proxy.example.com:8443",
		},
	}
	InitNetwork(cfg)

	got := GetProxyForURL("https://api.example.com")
	if got != "http://https-proxy.example.com:8443" {
		t.Errorf("expected https proxy to take precedence, got %q", got)
	}
}

// TestGetProxyForURLNoProxy verifies GetProxyForURL returns empty string when
// no proxy has been configured.
func TestGetProxyForURLNoProxy(t *testing.T) {
	resetGlobals()

	InitNetwork(nil)

	got := GetProxyForURL("https://api.example.com")
	if got != "" {
		t.Errorf("expected empty proxy, got %q", got)
	}
}

// TestGetProxyForURLInvalidTarget verifies GetProxyForURL handles a malformed
// URL without panicking, returning an empty string.
func TestGetProxyForURLInvalidTarget(t *testing.T) {
	resetGlobals()

	cfg := &types.NetworkConfig{
		Proxy: &types.ProxyConfig{
			HttpsProxy: "http://proxy.example.com:8080",
		},
	}
	InitNetwork(cfg)

	got := GetProxyForURL("://not a valid url")
	if got != "" {
		t.Errorf("expected empty string for invalid URL, got %q", got)
	}
}
