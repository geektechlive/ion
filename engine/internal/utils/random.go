// Random helpers for engine-internal observability identifiers.
//
// RandomID generates a short hex string suitable for correlating
// observability events (webhook request id, schedule fire id). Not
// cryptographically meaningful — just a stable handle a log reader can
// grep for. crypto/rand provides the bytes so collisions across
// concurrent processes are effectively impossible.

package utils

import (
	"crypto/rand"
	"encoding/hex"
)

// RandomID returns a 16-character hex string (8 random bytes). Used
// for engine_*_received → engine_*_responded correlation ids and
// similar internal handles where collision-resistance matters but
// cryptographic strength does not.
//
// On the (extremely unlikely) failure to read from crypto/rand, returns
// the string "0000000000000000" so callers can still log a value
// without short-circuiting their hot path. The fallback is logged at
// Warn so the operator sees the failure.
func RandomID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		Warn("utils", "RandomID: crypto/rand read failed: "+err.Error())
		return "0000000000000000"
	}
	return hex.EncodeToString(b[:])
}
