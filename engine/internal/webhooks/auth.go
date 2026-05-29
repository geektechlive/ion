// HMAC signature verification for webhook routes that use
// AuthHmacSignature. SHA-256 only — the WebhookAuth.Validate gate
// rejects any other algorithm at declaration time so this file does
// not need to fan out.
//
// Comparison uses crypto/hmac.Equal (which itself uses ConstantTime-
// compare internally) so a header that's the wrong length still
// rejects in constant time.
//
// Encoding: hex-lowercase is the de facto standard for HMAC headers
// (GitHub, Stripe, Slack all use either bare hex or "sha256=<hex>"
// prefix). The verifier accepts both shapes — bare hex or
// "sha256=<hex>" — so extensions don't have to pre-strip on the
// sender side.

package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// verifyHmacSha256 returns true when the provided signature header
// matches HMAC-SHA256(body, secret). Tolerates a "sha256=" prefix
// (case-insensitive) as commonly used by GitHub/Slack-style senders.
func verifyHmacSha256(body, secret []byte, header string) bool {
	if header == "" {
		return false
	}
	// Strip the optional "sha256=" prefix.
	if i := strings.IndexByte(header, '='); i > 0 && strings.EqualFold(header[:i], "sha256") {
		header = header[i+1:]
	}
	provided, err := hex.DecodeString(header)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(provided, expected)
}
