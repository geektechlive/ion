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
//
// This is a var (not a const) purely so tests can lower it via a defer-restored
// hook and exercise the size gates on tiny inputs; the production default is
// never weakened. Same rationale for stageTotalMaxBytes and stageMaxConvDirs.
var maxStageAttachmentBytes = 36 * 1024 * 1024

// stageAttachmentTTL is how long a staged file survives before an opportunistic
// prune removes it. Staging is a scratch mechanism, not durable storage; the
// consumer is expected to move or copy the bytes promptly.
const stageAttachmentTTL = 7 * 24 * time.Hour

// stageTotalMaxBytes caps the TOTAL bytes allowed under the attachments root
// across all conversation dirs. Without it, a client staging files across many
// unique conversation Keys (fresh conv dirs) could fill the host disk — the
// per-file cap alone does not bound aggregate usage. Test-adjustable var; the
// production default is never weakened.
var stageTotalMaxBytes int64 = 512 * 1024 * 1024

// stageMaxConvDirs caps the number of conversation dirs under the attachments
// root, bounding inode/dir growth independently of byte size. Test-adjustable
// var; the production default is never weakened.
var stageMaxConvDirs = 512

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
//   - Confinement is then re-verified after symlink resolution (EvalSymlinks +
//     Rel): the lexical prefix check above is defeated by a pre-existing
//     symlinked conv dir, so the resolved paths are compared before any write.
//   - Two resource caps bound abuse the per-file size cap cannot: a global byte
//     quota (stageTotalMaxBytes) and a conversation-dir count cap
//     (stageMaxConvDirs), enforced after an opportunistic global TTL prune.
//   - The encoded (pre-decode) length is rejected before base64 decode allocates
//     ~48 MB for an over-cap payload.
//
// The engine owns only the generic mechanism (accept bytes, write to a scratch
// dir, return a host path). The consumer decides what to do with the staged
// file; the engine holds no opinion.
func stageAttachment(convID, filename, mimeType, dataB64 string) (map[string]interface{}, error) {
	utils.Debug("FS", fmt.Sprintf("stage_attachment: convID=%q filename=%q mimeType=%q b64Len=%d", convID, filename, mimeType, len(dataB64)))

	// 0. Pre-decode size gate (Finding 2 — memory amplification). base64 decode
	// allocates the full ~48 MB buffer for an over-cap payload BEFORE the decoded
	// size can be checked. Reject on the encoded length first, so an over-cap
	// payload never reaches DecodeString. The post-decode check below stays as a
	// belt-and-suspenders bound (reachable only at the exact boundary).
	maxEncoded := base64.StdEncoding.EncodedLen(maxStageAttachmentBytes)
	if len(dataB64) > maxEncoded {
		utils.Error("FS", fmt.Sprintf("stage_attachment: encoded payload too large convID=%q filename=%q b64Len=%d maxEncoded=%d maxDecoded=%d", convID, filename, len(dataB64), maxEncoded, maxStageAttachmentBytes))
		return nil, fmt.Errorf("encoded attachment exceeds maximum size of %d bytes (encoded length %d exceeds limit %d)", maxStageAttachmentBytes, len(dataB64), maxEncoded)
	}

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

	// 3b. Global quota + inode cap (Finding 1 — unbounded disk/inode DoS). The
	// per-file cap does not bound aggregate usage; a client staging across unique
	// conversation Keys could fill the host disk. Reclaim first (global TTL prune
	// across ALL conv dirs, not just this one), then measure and reject over-cap
	// BEFORE writing anything.
	if err := os.MkdirAll(root, 0o700); err != nil {
		utils.Error("FS", fmt.Sprintf("stage_attachment: mkdir root failed root=%q err=%v", root, err))
		return nil, fmt.Errorf("failed to create attachment root: %w", err)
	}
	pruneStaleAttachmentsGlobal(root)
	totalBytes, dirCount := stageRootUsage(root)
	// Only count the new conv dir against the dir cap when it does not already
	// exist (an existing dir adds no inode pressure).
	effectiveDirs := dirCount
	convDirExists := false
	if _, statErr := os.Stat(convDir); statErr == nil {
		convDirExists = true
	} else {
		effectiveDirs++
	}
	projectedBytes := totalBytes + int64(len(decoded))
	if projectedBytes > stageTotalMaxBytes || effectiveDirs >= stageMaxConvDirs {
		utils.Error("FS", fmt.Sprintf("stage_attachment: quota exceeded convID=%q seg=%q totalBytes=%d addBytes=%d projected=%d maxBytes=%d dirCount=%d effectiveDirs=%d maxDirs=%d convDirExists=%v", convID, convSeg, totalBytes, len(decoded), projectedBytes, stageTotalMaxBytes, dirCount, effectiveDirs, stageMaxConvDirs, convDirExists))
		return nil, fmt.Errorf("attachment storage quota exceeded (projected %d bytes over %d, or %d conversation dirs over %d)", projectedBytes, stageTotalMaxBytes, effectiveDirs, stageMaxConvDirs)
	}
	utils.Debug("FS", fmt.Sprintf("stage_attachment: quota ok convID=%q seg=%q totalBytes=%d projected=%d maxBytes=%d effectiveDirs=%d maxDirs=%d", convID, convSeg, totalBytes, projectedBytes, stageTotalMaxBytes, effectiveDirs, stageMaxConvDirs))

	// 4. Write.
	if err := os.MkdirAll(convDir, 0o700); err != nil {
		utils.Error("FS", fmt.Sprintf("stage_attachment: mkdir failed dir=%q err=%v", convDir, err))
		return nil, fmt.Errorf("failed to create attachment directory: %w", err)
	}

	// Finding 3 — symlink confinement. The lexical HasPrefix check above is
	// purely textual and does not resolve symlinks; MkdirAll/WriteFile FOLLOW
	// them, so a pre-existing symlinked conv dir (or a symlinked path component)
	// could escape the root while the string check passes. Re-verify containment
	// on the SYMLINK-RESOLVED paths, mirroring get_plan_content
	// (dispatch_plan_content.go). Rel is used on both sides because macOS aliases
	// (/var -> /private/var) make a raw prefix compare unreliable. If EvalSymlinks
	// errors (transient), fall back to the lexical check already done above.
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedConvDir, convErr := filepath.EvalSymlinks(convDir)
	if rootErr == nil && convErr == nil {
		rel, relErr := filepath.Rel(resolvedRoot, resolvedConvDir)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			utils.Error("FS", fmt.Sprintf("stage_attachment: resolved conv dir escapes root (symlink) convID=%q convDir=%q resolvedConvDir=%q resolvedRoot=%q rel=%q relErr=%v", convID, convDir, resolvedConvDir, resolvedRoot, rel, relErr))
			return nil, fmt.Errorf("resolved attachment path escapes the confinement root")
		}
		utils.Debug("FS", fmt.Sprintf("stage_attachment: symlink-resolved containment ok resolvedRoot=%q resolvedConvDir=%q rel=%q", resolvedRoot, resolvedConvDir, rel))
	} else {
		utils.Debug("FS", fmt.Sprintf("stage_attachment: EvalSymlinks fell back to lexical convDir=%q rootErr=%v convErr=%v", convDir, rootErr, convErr))
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

// pruneStaleAttachmentsGlobal sweeps EVERY conversation dir under the root,
// removing files older than stageAttachmentTTL and then removing any conv dir
// left empty. The per-dir pruneStaleAttachments only touches the single dir
// being written, so it is inert against a client that fills the disk across many
// unique conv dirs; this global sweep is what makes the TTL a real reclaim.
// Opportunistic and non-fatal: every failure is logged and skipped. Does NOT
// follow symlinked entries (ReadDir reports a symlink as a non-dir, so a
// symlinked conv dir is skipped, never traversed).
func pruneStaleAttachmentsGlobal(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		utils.Debug("FS", fmt.Sprintf("stage_attachment global prune: readdir root skipped root=%q err=%v", root, err))
		return
	}
	dirsSwept := 0
	dirsRemoved := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		convDir := filepath.Join(root, e.Name())
		pruneStaleAttachments(convDir)
		dirsSwept++
		remaining, rerr := os.ReadDir(convDir)
		if rerr != nil {
			utils.Debug("FS", fmt.Sprintf("stage_attachment global prune: readdir after-prune skipped dir=%q err=%v", convDir, rerr))
			continue
		}
		if len(remaining) != 0 {
			continue
		}
		if rmErr := os.Remove(convDir); rmErr != nil {
			utils.Debug("FS", fmt.Sprintf("stage_attachment global prune: rmdir failed dir=%q err=%v", convDir, rmErr))
			continue
		}
		dirsRemoved++
		utils.Log("FS", fmt.Sprintf("stage_attachment global prune: removed empty conv dir=%q", convDir))
	}
	if dirsSwept > 0 {
		utils.Debug("FS", fmt.Sprintf("stage_attachment global prune: root=%q dirsSwept=%d dirsRemoved=%d", root, dirsSwept, dirsRemoved))
	}
}

// stageRootUsage returns the total size of all regular files under the root and
// the count of immediate conversation subdirs. Conv files live exactly one level
// deep (root/<convSeg>/<file>), so a two-level walk is sufficient and avoids
// following symlinks. Non-fatal: unreadable entries are skipped (logged at debug)
// and contribute zero, so a transient stat error under-counts rather than failing
// the stage.
func stageRootUsage(root string) (totalBytes int64, dirCount int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		utils.Debug("FS", fmt.Sprintf("stage_attachment usage: readdir root skipped root=%q err=%v", root, err))
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			dirCount++
			sub, serr := os.ReadDir(filepath.Join(root, e.Name()))
			if serr != nil {
				utils.Debug("FS", fmt.Sprintf("stage_attachment usage: readdir subdir skipped name=%q err=%v", e.Name(), serr))
				continue
			}
			for _, f := range sub {
				if f.IsDir() {
					continue
				}
				info, ierr := f.Info()
				if ierr != nil {
					utils.Debug("FS", fmt.Sprintf("stage_attachment usage: stat skipped name=%q err=%v", f.Name(), ierr))
					continue
				}
				totalBytes += info.Size()
			}
			continue
		}
		// Regular file directly under the root (unexpected layout, still counted).
		info, ierr := e.Info()
		if ierr != nil {
			utils.Debug("FS", fmt.Sprintf("stage_attachment usage: stat root-file skipped name=%q err=%v", e.Name(), ierr))
			continue
		}
		totalBytes += info.Size()
	}
	return totalBytes, dirCount
}
