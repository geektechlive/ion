package server

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stageRoot returns the confinement root for the current HOME.
func stageRoot(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return filepath.Clean(filepath.Join(home, ".ion", "attachments"))
}

// withTempHome points HOME at a fresh temp dir so stageAttachment writes into
// an isolated tree we can assert against and t.TempDir cleans up.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestStageAttachmentHappyPath(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	payload := []byte("hello staged bytes")
	b64 := base64.StdEncoding.EncodeToString(payload)

	resp, err := stageAttachment("conv-123", "notes.txt", "text/plain", b64)
	if err != nil {
		t.Fatalf("stageAttachment err: %v", err)
	}
	path, _ := resp["path"].(string)
	if path == "" {
		t.Fatal("expected non-empty path in response")
	}
	// Path must sit under the confinement root, inside the conv segment.
	if !strings.HasPrefix(path, filepath.Join(root, "conv-123")+string(os.PathSeparator)) {
		t.Errorf("path %q not under conv dir of root %q", path, root)
	}
	// UUID-prefixed basename ending in the original filename.
	if !strings.HasSuffix(path, "-notes.txt") {
		t.Errorf("path %q does not end with -notes.txt", path)
	}
	// Bytes round-trip.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("round-trip mismatch: got %q want %q", got, payload)
	}
	// File mode is 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o want 600", perm)
	}
}

func TestStageAttachmentTraversalFilename(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	cases := []struct {
		name     string
		filename string
	}{
		{"dotdot-path", "../../etc/passwd"},
		{"absolute", "/etc/passwd"},
		{"nested", "a/b/c.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b64 := base64.StdEncoding.EncodeToString([]byte("x"))
			resp, err := stageAttachment("conv", tc.filename, "application/octet-stream", b64)
			if err != nil {
				t.Fatalf("stageAttachment err: %v", err)
			}
			path, _ := resp["path"].(string)
			// Never escapes the root.
			if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
				t.Fatalf("path %q escaped root %q", path, root)
			}
			// Stored under the conv segment, using only the basename.
			wantBase := filepath.Base(tc.filename)
			if !strings.HasSuffix(path, "-"+wantBase) {
				t.Errorf("path %q does not end with basename %q", path, wantBase)
			}
			// Confirm the file physically exists at the confined location and
			// that no file was written at the traversal target.
			if _, statErr := os.Stat(path); statErr != nil {
				t.Errorf("expected staged file at %q: %v", path, statErr)
			}
			if _, statErr := os.Stat("/etc/passwd-staged-marker"); statErr == nil {
				t.Errorf("unexpected write outside root")
			}
		})
	}
}

func TestStageAttachmentTraversalConvID(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	b64 := base64.StdEncoding.EncodeToString([]byte("x"))
	resp, err := stageAttachment("../../foo", "f.txt", "text/plain", b64)
	if err != nil {
		t.Fatalf("stageAttachment err: %v", err)
	}
	path, _ := resp["path"].(string)
	if !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		t.Fatalf("convID traversal escaped root: path=%q root=%q", path, root)
	}
	// filepath.Base("../../foo") == "foo": stored under root/foo/...
	if !strings.HasPrefix(path, filepath.Join(root, "foo")+string(os.PathSeparator)) {
		t.Errorf("expected conv segment 'foo', path=%q", path)
	}
}

func TestStageAttachmentConvIDCollapsesToDefault(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	b64 := base64.StdEncoding.EncodeToString([]byte("x"))
	// convID ".." collapses under sanitize -> "default" segment.
	resp, err := stageAttachment("..", "f.txt", "text/plain", b64)
	if err != nil {
		t.Fatalf("stageAttachment err: %v", err)
	}
	path, _ := resp["path"].(string)
	if !strings.HasPrefix(path, filepath.Join(root, "default")+string(os.PathSeparator)) {
		t.Errorf("expected 'default' segment for collapsing convID, path=%q", path)
	}
}

func TestStageAttachmentEmptyFilenameFallback(t *testing.T) {
	withTempHome(t)

	b64 := base64.StdEncoding.EncodeToString([]byte("x"))
	resp, err := stageAttachment("conv", "", "text/plain", b64)
	if err != nil {
		t.Fatalf("stageAttachment err: %v", err)
	}
	path, _ := resp["path"].(string)
	if !strings.HasSuffix(path, "-attachment") {
		t.Errorf("expected fallback basename 'attachment', path=%q", path)
	}
}

func TestStageAttachmentOversized(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	// One byte over the cap, base64-encoded.
	big := make([]byte, maxStageAttachmentBytes+1)
	b64 := base64.StdEncoding.EncodeToString(big)

	_, err := stageAttachment("conv-big", "big.bin", "application/octet-stream", b64)
	if err == nil {
		t.Fatal("expected error for oversized attachment")
	}
	if !strings.Contains(err.Error(), "maximum size") {
		t.Errorf("error %q does not mention size cap", err)
	}
	// Nothing written: the conv dir must not exist.
	if _, statErr := os.Stat(filepath.Join(root, "conv-big")); statErr == nil {
		t.Error("oversized attachment wrote a file / created its dir")
	}
}

func TestStageAttachmentInvalidBase64(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	_, err := stageAttachment("conv-bad", "x.txt", "text/plain", "!!!not base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("error %q does not mention base64", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "conv-bad")); statErr == nil {
		t.Error("invalid base64 wrote a file / created its dir")
	}
}

func TestStageAttachmentPrunesStaleFiles(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	// Pre-seed a stale file in the conv dir.
	convDir := filepath.Join(root, "conv-prune")
	if err := os.MkdirAll(convDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(convDir, "old-file.txt")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	old := time.Now().Add(-stageAttachmentTTL - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Staging a new file triggers the opportunistic prune.
	b64 := base64.StdEncoding.EncodeToString([]byte("fresh"))
	resp, err := stageAttachment("conv-prune", "fresh.txt", "text/plain", b64)
	if err != nil {
		t.Fatalf("stageAttachment err: %v", err)
	}
	if _, statErr := os.Stat(stale); statErr == nil {
		t.Error("stale file was not pruned")
	}
	// The freshly staged file survives.
	newPath, _ := resp["path"].(string)
	if _, statErr := os.Stat(newPath); statErr != nil {
		t.Errorf("fresh file missing after prune: %v", statErr)
	}
}

// TestStageAttachmentGlobalQuota (Finding 1) proves the aggregate byte quota
// rejects an over-cap stage and writes nothing. Fails on the unfixed handler,
// which has no quota check and lets the second stage succeed.
func TestStageAttachmentGlobalQuota(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	// Lower ONLY the byte quota so two small payloads trip it; leave the dir cap
	// at its default so this test isolates the byte path.
	orig := stageTotalMaxBytes
	stageTotalMaxBytes = 60
	defer func() { stageTotalMaxBytes = orig }()

	payload := make([]byte, 40) // 40 bytes; two of these exceed 60.
	b64 := base64.StdEncoding.EncodeToString(payload)

	// First stage: total 0 -> 40, under 60. Succeeds.
	if _, err := stageAttachment("conv-a", "a.bin", "application/octet-stream", b64); err != nil {
		t.Fatalf("first stage should succeed: %v", err)
	}

	// Second stage into a DIFFERENT conv dir: 40 + 40 = 80 > 60. Rejected.
	_, err := stageAttachment("conv-b", "b.bin", "application/octet-stream", b64)
	if err == nil {
		t.Fatal("expected quota error for over-cap stage")
	}
	if !strings.Contains(err.Error(), "quota") {
		t.Errorf("error %q does not mention quota", err)
	}
	// Nothing written for the rejected stage: its conv dir must not exist.
	if _, statErr := os.Stat(filepath.Join(root, "conv-b")); statErr == nil {
		t.Error("over-quota stage created its conv dir / wrote a file")
	}
}

// TestStageAttachmentDirCap (Finding 1) proves the conversation-dir cap rejects
// a new dir once the cap is reached, while still allowing a re-stage into an
// EXISTING dir (which adds no inode pressure). Fails on the unfixed handler.
func TestStageAttachmentDirCap(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	orig := stageMaxConvDirs
	stageMaxConvDirs = 3 // effectiveDirs >= 3 rejects; allows 2 distinct dirs.
	defer func() { stageMaxConvDirs = orig }()

	b64 := base64.StdEncoding.EncodeToString([]byte("x"))

	// Two distinct conv dirs succeed (effectiveDirs 1, then 2).
	if _, err := stageAttachment("conv-1", "f.txt", "text/plain", b64); err != nil {
		t.Fatalf("conv-1 should succeed: %v", err)
	}
	if _, err := stageAttachment("conv-2", "f.txt", "text/plain", b64); err != nil {
		t.Fatalf("conv-2 should succeed: %v", err)
	}

	// Re-staging into an EXISTING dir is allowed at the cap (dir not counted).
	if _, err := stageAttachment("conv-1", "g.txt", "text/plain", b64); err != nil {
		t.Fatalf("re-stage into existing conv-1 should succeed: %v", err)
	}

	// A THIRD distinct dir trips the cap (effectiveDirs 3 >= 3).
	_, err := stageAttachment("conv-3", "f.txt", "text/plain", b64)
	if err == nil {
		t.Fatal("expected dir-cap error for third distinct conv dir")
	}
	if !strings.Contains(err.Error(), "quota") {
		t.Errorf("error %q does not mention quota", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "conv-3")); statErr == nil {
		t.Error("over-cap stage created its conv dir")
	}
}

// TestStageAttachmentGlobalPruneReclaims (Finding 1) proves the prune sweep is
// GLOBAL: a stale file in a DIFFERENT conv dir than the one being written is
// reclaimed. Fails on the unfixed handler, whose per-dir prune only touches the
// dir being written.
func TestStageAttachmentGlobalPruneReclaims(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	// Pre-seed a stale file in an OTHER conv dir (not the one we will stage into).
	otherDir := filepath.Join(root, "conv-other")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	stale := filepath.Join(otherDir, "old.txt")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	old := time.Now().Add(-stageAttachmentTTL - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Stage into a DIFFERENT conv dir; the global prune must reclaim the stale
	// file (and remove the now-empty other dir).
	b64 := base64.StdEncoding.EncodeToString([]byte("fresh"))
	if _, err := stageAttachment("conv-current", "fresh.txt", "text/plain", b64); err != nil {
		t.Fatalf("stageAttachment err: %v", err)
	}
	if _, statErr := os.Stat(stale); statErr == nil {
		t.Error("stale file in OTHER conv dir was not pruned (sweep is not global)")
	}
	if _, statErr := os.Stat(otherDir); statErr == nil {
		t.Error("now-empty other conv dir was not removed by global prune")
	}
}

// TestStageAttachmentPreDecodeSizeGate (Finding 2) proves the encoded length is
// rejected BEFORE base64 decode runs. The input is over-length AND invalid
// base64: the fixed handler rejects on length (error mentions "maximum size")
// without decoding; the unfixed handler reaches DecodeString and returns an
// "invalid base64" error instead — so the "maximum size" assertion fails on the
// unfixed code.
func TestStageAttachmentPreDecodeSizeGate(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	// Lower the per-file cap so a tiny input exceeds the encoded limit — this is
	// exactly the memory-amplification the finding targets (no 48 MB alloc).
	orig := maxStageAttachmentBytes
	maxStageAttachmentBytes = 12 // EncodedLen(12) == 16.
	defer func() { maxStageAttachmentBytes = orig }()

	// 20 chars of an invalid base64 byte: over the 16-char encoded limit AND not
	// decodable. Proves the length gate fires before any decode attempt.
	bad := strings.Repeat("!", 20)

	_, err := stageAttachment("conv-amp", "x.bin", "application/octet-stream", bad)
	if err == nil {
		t.Fatal("expected pre-decode size error")
	}
	if !strings.Contains(err.Error(), "maximum size") {
		t.Errorf("error %q is not the pre-decode size error (decode ran first?)", err)
	}
	if strings.Contains(err.Error(), "invalid base64") {
		t.Errorf("decode ran before the size gate: err=%q", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "conv-amp")); statErr == nil {
		t.Error("pre-decode-rejected stage wrote a file / created its dir")
	}
}

// TestStageAttachmentSymlinkEscape (Finding 3) pre-creates the conv dir as a
// SYMLINK pointing OUTSIDE the confinement root. The lexical HasPrefix check
// passes (the string is still under root), but the symlink-resolved containment
// check must reject. Fails on the unfixed handler, whose WriteFile follows the
// symlink and lands the file at the escape target.
func TestStageAttachmentSymlinkEscape(t *testing.T) {
	withTempHome(t)
	root := stageRoot(t)

	// A second temp dir OUTSIDE the confinement root is the escape target.
	escape := t.TempDir()

	// Create root, then pre-create the conv dir as a symlink -> escape target.
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	convDir := filepath.Join(root, "conv-eviltarget")
	if err := os.Symlink(escape, convDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	b64 := base64.StdEncoding.EncodeToString([]byte("payload"))
	_, err := stageAttachment("conv-eviltarget", "loot.txt", "text/plain", b64)
	if err == nil {
		t.Fatal("expected symlink-confinement error")
	}
	if !strings.Contains(err.Error(), "confinement root") {
		t.Errorf("error %q is not the confinement error", err)
	}
	// Nothing was written at the symlink target.
	targetEntries, readErr := os.ReadDir(escape)
	if readErr != nil {
		t.Fatalf("readdir escape target: %v", readErr)
	}
	if len(targetEntries) != 0 {
		t.Errorf("symlink escape wrote %d entries at target %q", len(targetEntries), escape)
	}
}

func TestNewStageIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := newStageID()
		if seen[id] {
			t.Fatalf("duplicate stage id: %q", id)
		}
		seen[id] = true
	}
}
