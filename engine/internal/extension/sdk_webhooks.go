// SDK declarations for webhook routes — surface registered by extensions
// at init time (bulk) or dynamically via the ext/register_webhook RPC.
//
// The declaration shape is the contract extensions emit to the engine.
// Field names match the JSON wire format the SDK runtime sends. Auth
// secrets are NOT carried inline; the extension declares an opaque
// TokenRef (a name the engine echoes back over an `engine/resolve_token`
// callback) so credentials never sit in registry memory longer than the
// fire window requires.
//
// Why a separate file from `host.go`: keeps the host's storage of
// per-host registrations conceptually adjacent to the registration
// type definitions without bloating either. The host_rpc.go RPC cases
// for ext/register_webhook unmarshal directly into these structs.

package extension

import "fmt"

// AuthKind names the supported webhook authentication strategies.
type AuthKind string

const (
	// AuthNone leaves the route unauthenticated. Only sensible for
	// loopback-bound listeners or extensions that explicitly opt into
	// public exposure (e.g. read-only health endpoints).
	AuthNone AuthKind = "none"
	// AuthBearer requires an `Authorization: Bearer <token>` header.
	// The token value is resolved lazily through the extension via
	// TokenRefName so secrets never live in engine memory longer than
	// necessary.
	AuthBearer AuthKind = "bearer"
	// AuthSharedSecret requires a custom header to equal a shared
	// secret value. HeaderName carries the header; TokenRefName resolves
	// the expected value.
	AuthSharedSecret AuthKind = "shared-secret"
	// AuthHmacSignature verifies an HMAC of the raw request body
	// against a signature provided in HeaderName. Algorithm names the
	// hash ("sha256" is the only supported value today). TokenRefName
	// resolves the shared HMAC key.
	AuthHmacSignature AuthKind = "hmac-signature"
)

// WebhookAuth declares the authentication strategy for a webhook route.
// The zero value (`Kind: ""`) is invalid; declarations must specify
// `Kind: "none"` explicitly to opt out of auth — a guardrail against
// accidental zero-value exposure of a sensitive route.
type WebhookAuth struct {
	// Kind selects the auth strategy. Required.
	Kind AuthKind `json:"kind"`
	// HeaderName is the request header carrying the token / signature
	// for shared-secret and hmac-signature. Ignored for bearer (always
	// reads `Authorization`) and none. Required when Kind requires it.
	HeaderName string `json:"headerName,omitempty"`
	// Algorithm names the HMAC hash for hmac-signature. Currently only
	// "sha256" is accepted. Required for hmac-signature, ignored
	// otherwise.
	Algorithm string `json:"algorithm,omitempty"`
	// TokenRefName is the symbolic name the engine echoes back over the
	// `engine/resolve_token` callback when it needs the actual secret.
	// The SDK runtime stores the user's `() => string` callback under
	// this name and replies with the resolved value. Required for
	// bearer, shared-secret, hmac-signature.
	TokenRefName string `json:"tokenRefName,omitempty"`
}

// Validate returns a non-nil error when the auth declaration is
// internally inconsistent. Callers use this to reject malformed
// registrations at the RPC boundary so the operator sees a clear
// message rather than a runtime auth failure.
func (a WebhookAuth) Validate() error {
	switch a.Kind {
	case AuthNone:
		return nil
	case AuthBearer:
		if a.TokenRefName == "" {
			return fmt.Errorf("webhook auth=bearer requires tokenRefName")
		}
		return nil
	case AuthSharedSecret:
		if a.HeaderName == "" {
			return fmt.Errorf("webhook auth=shared-secret requires headerName")
		}
		if a.TokenRefName == "" {
			return fmt.Errorf("webhook auth=shared-secret requires tokenRefName")
		}
		return nil
	case AuthHmacSignature:
		if a.HeaderName == "" {
			return fmt.Errorf("webhook auth=hmac-signature requires headerName")
		}
		if a.Algorithm != "sha256" {
			return fmt.Errorf("webhook auth=hmac-signature only supports algorithm=sha256 (got %q)", a.Algorithm)
		}
		if a.TokenRefName == "" {
			return fmt.Errorf("webhook auth=hmac-signature requires tokenRefName")
		}
		return nil
	default:
		return fmt.Errorf("unknown webhook auth kind %q", a.Kind)
	}
}

// WebhookRoute is the full registration declaration for an HTTP route.
// Method defaults to POST (server-side; the wire payload may omit it).
// MaxBodyBytes is enforced before any handler dispatch so a buggy or
// malicious sender cannot tie up the engine reading multi-megabyte
// payloads when the extension only needs the first 4kB.
type WebhookRoute struct {
	// Path is the URL path the engine listens on. Must start with "/".
	// Two routes with the same path on the same host are rejected by
	// the registry as duplicates.
	Path string `json:"path"`
	// Method is the HTTP method ("GET", "POST", "PUT", ...). Empty
	// defaults to "POST" at dispatch time. Cross-method ambiguity on
	// the same path is intentionally allowed — register one route per
	// (path, method) pair if you need that distinction.
	Method string `json:"method,omitempty"`
	// Auth declares the authentication strategy. Required.
	Auth WebhookAuth `json:"auth"`
	// MaxBodyBytes caps the request body the engine will read before
	// 413-ing. Zero means inherit the engine config default. A negative
	// value disables the cap (not recommended in production).
	MaxBodyBytes int64 `json:"maxBodyBytes,omitempty"`
	// Interface is the bind interface for the listener. Empty inherits
	// the engine config default (typically 127.0.0.1). The engine logs
	// a loud Warn when a non-loopback interface is configured.
	Interface string `json:"interface,omitempty"`
}

// ID satisfies the asyncreg.Declaration interface. Webhook routes use
// the path as their stable identifier within the registry.
func (r WebhookRoute) ID() string { return r.Path }
