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
