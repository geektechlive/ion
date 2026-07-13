package server

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dsswift/ion/engine/internal/utils"
)

// maxStageAttachmentBytes caps the decoded size of a staged attachment. It sits
// below the 64 MB NDJSON frame limit (scanner.Buffer in server.go) with base64
// headroom (base64 inflates by ~33%, so a 36 MB file is ~48 MB on the wire).
const maxStageAttachmentBytes = 36 * 1024 * 1024

// stageAttachmentTTL is how long a staged file survives before an opportunistic
// prune removes it. Staging is a scratch mechanism, not durable storage; the
// consumer is expected to move or copy the bytes promptly.
const stageAttachmentTTL = 7 * 24 * time.Hour

// stageAttachment accepts base64-encoded file bytes from a client and writes
// them to a confined per-conversation scratch directory on the engine host,
// returning the absolute host path.
//
// This is the FIRST client-facing host-WRITE command. Every other host-FS
// command (get_host_info, list_directory) is read-only. The confinement logic
// below is therefore load-bearing security, not defense-in-depth decoration:
//
//   - Root is <UserHomeDir>/.ion/attachments/.
//   - convID is reduced to a single safe path segment (filepath.Base, with a
//     fallback when it collapses to "", ".", or "..").
//   - filename is reduced to its basename only (all directory components
//     stripped), with a fallback default. The stored name is UUID-prefixed to
//     avoid collisions.
//   - The final path is cleaned and verified to still sit under the root before
//     any write happens — a path that escapes is rejected with nothing written.
//
// The engine owns only the generic mechanism (accept bytes, write to a scratch
// dir, return a host path). The consumer decides what to do with the staged
// file; the engine holds no opinion.
func stageAttachment(convID, filename, mimeType, dataB64 string) (map[string]interface{}, error) {
	utils.Debug("FS", fmt.Sprintf("stage_attachment: convID=%q filename=%q mimeType=%q b64Len=%d", convID, filename, mimeType, len(dataB64)))

	// 1. Decode base64.
	decoded, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		utils.Error("FS", fmt.Sprintf("stage_attachment: base64 decode failed convID=%q filename=%q err=%v", convID, filename, err))
		return nil, fmt.Errorf("invalid base64 data: %w", err)
	}

	// 2. Size cap — reject before writing anything.
	if len(decoded) > maxStageAttachmentBytes {
		utils.Error("FS", fmt.Sprintf("stage_attachment: payload too large convID=%q filename=%q bytes=%d max=%d", convID, filename, len(decoded), maxStageAttachmentBytes))
		return nil, fmt.Errorf("attachment exceeds maximum size of %d bytes (got %d)", maxStageAttachmentBytes, len(decoded))
	}

	// 3. Confinement.
	home, err := os.UserHomeDir()
	if err != nil {
		utils.Error("FS", fmt.Sprintf("stage_attachment: home directory unavailable convID=%q err=%v", convID, err))
		return nil, fmt.Errorf("home directory unavailable: %w", err)
	}
	root := filepath.Clean(filepath.Join(home, ".ion", "attachments"))

	// Sanitize convID to a single safe path segment. filepath.Base strips any
	// directory components (so "../../foo" becomes "foo"); the residual danger
	// is "." / ".." / "" which we replace with a stable default segment.
	convSeg := filepath.Base(convID)
	if convSeg == "" || convSeg == "." || convSeg == ".." || convSeg == string(os.PathSeparator) {
		utils.Log("FS", fmt.Sprintf("stage_attachment: convID %q collapsed under sanitize; using default segment", convID))
		convSeg = "default"
	}

	// Sanitize filename to its basename ONLY; fall back when it collapses.
	base := filepath.Base(filename)
	if base == "" || base == "." || base == ".." || base == string(os.PathSeparator) {
		utils.Log("FS", fmt.Sprintf("stage_attachment: filename %q collapsed under sanitize; using default name", filename))
		base = "attachment"
	}

	// UUID prefix avoids collisions when the same basename is staged twice.
	storedName := newStageID() + "-" + base
	convDir := filepath.Join(root, convSeg)
	finalPath := filepath.Clean(filepath.Join(convDir, storedName))

	// Defense-in-depth: the cleaned path must still be strictly under the root.
	// The trailing separator prevents a sibling like ".ion/attachments-evil"
	// from passing a bare-prefix check.
	if !strings.HasPrefix(finalPath, root+string(os.PathSeparator)) {
		utils.Error("FS", fmt.Sprintf("stage_attachment: resolved path escapes root convID=%q filename=%q finalPath=%q root=%q", convID, filename, finalPath, root))
		return nil, fmt.Errorf("resolved attachment path escapes the confinement root")
	}

	// 4. Write.
	if err := os.MkdirAll(convDir, 0o700); err != nil {
		utils.Error("FS", fmt.Sprintf("stage_attachment: mkdir failed dir=%q err=%v", convDir, err))
		return nil, fmt.Errorf("failed to create attachment directory: %w", err)
	}
	if err := os.WriteFile(finalPath, decoded, 0o600); err != nil {
		utils.Error("FS", fmt.Sprintf("stage_attachment: write failed path=%q err=%v", finalPath, err))
		return nil, fmt.Errorf("failed to write attachment: %w", err)
	}
	utils.Log("FS", fmt.Sprintf("stage_attachment: wrote convID=%q seg=%q path=%q bytes=%d mimeType=%q", convID, convSeg, finalPath, len(decoded), mimeType))

	// 5. Best-effort TTL prune of stale files in this conversation dir. Never
	// fatal — a prune failure must not fail the stage that just succeeded.
	pruneStaleAttachments(convDir)

	return map[string]interface{}{"path": finalPath}, nil
}

// newStageID returns a random UUIDv4-formatted string used to prefix stored
// attachment names. Mirrors the crypto/rand idiom in conversation/id.go: on the
// vanishingly rare rand failure it falls back to a nanosecond timestamp, which
// is still unique enough for collision avoidance within a conversation dir.
func newStageID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// pruneStaleAttachments removes files older than stageAttachmentTTL from a
// conversation's scratch dir. Opportunistic and non-fatal: every failure is
// logged at debug level and skipped so the caller's stage always succeeds.
func pruneStaleAttachments(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		utils.Debug("FS", fmt.Sprintf("stage_attachment prune: readdir skipped dir=%q err=%v", dir, err))
		return
	}
	cutoff := time.Now().Add(-stageAttachmentTTL)
	pruned := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			utils.Debug("FS", fmt.Sprintf("stage_attachment prune: stat skipped name=%q err=%v", e.Name(), err))
			continue
		}
		if info.ModTime().Before(cutoff) {
			p := filepath.Join(dir, e.Name())
			if err := os.Remove(p); err != nil {
				utils.Debug("FS", fmt.Sprintf("stage_attachment prune: remove failed path=%q err=%v", p, err))
				continue
			}
			pruned++
			utils.Log("FS", fmt.Sprintf("stage_attachment prune: removed stale path=%q modTime=%s", p, info.ModTime().Format(time.RFC3339)))
		}
	}
	if pruned > 0 {
		utils.Log("FS", fmt.Sprintf("stage_attachment prune: dir=%q removed=%d cutoff=%s", dir, pruned, cutoff.Format(time.RFC3339)))
	}
}
