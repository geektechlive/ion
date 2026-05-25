// Persistence of last-run markers for daily / weekly schedule jobs.
// Used by the catch-up logic so a restart can determine whether a
// scheduled slot was missed while the engine was down.
//
// Format: one JSON file per (host, job) under PersistDir/<safeName>.
// File contents: {"lastRunUtc": "2026-01-15T09:30:00Z"}. The format is
// deliberately trivial — no schema versioning needed at this scope;
// future iterations can add fields with omitempty.
//
// Failures (mkdir, write, parse) log at Warn but never abort the
// scheduler — losing a marker just means the next catch-up sweep
// might fire the same slot twice, which is preferable to silent
// scheduler failure.

package scheduling

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/utils"
)

// lastRunMarker is the on-disk shape. Single field keeps the file
// trivially small (under 80 bytes per job).
type lastRunMarker struct {
	LastRunUtc string `json:"lastRunUtc"`
}

// recordLastRun writes the marker for a successful fire. No-op if
// persistDir is empty (tests, or persistence disabled by config).
// Interval jobs do not write markers — they don't catch up.
func (s *Scheduler) recordLastRun(h *extension.Host, job extension.ScheduleJob, firedAt time.Time) {
	if s.persistDir == "" {
		return
	}
	if job.Kind == extension.ScheduleInterval {
		return
	}
	path := s.markerPath(h, job)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		utils.Warn("scheduler", fmt.Sprintf("recordLastRun: mkdir %s: %v", filepath.Dir(path), err))
		return
	}
	data, err := json.Marshal(lastRunMarker{LastRunUtc: firedAt.UTC().Format(time.RFC3339)})
	if err != nil {
		utils.Warn("scheduler", fmt.Sprintf("recordLastRun: marshal %s: %v", path, err))
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		utils.Warn("scheduler", fmt.Sprintf("recordLastRun: write %s: %v", path, err))
		return
	}
	utils.Debug("scheduler", fmt.Sprintf("recordLastRun: ext=%s id=%q wrote %s", h.Name(), job.JobID, path))
}

// readLastRun reads the marker if it exists. Returns (zero, false)
// when the file is missing, malformed, or persistence is off.
func (s *Scheduler) readLastRun(h *extension.Host, job extension.ScheduleJob) (time.Time, bool) {
	if s.persistDir == "" {
		return time.Time{}, false
	}
	path := s.markerPath(h, job)
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			utils.Debug("scheduler", fmt.Sprintf("readLastRun: read %s: %v", path, err))
		}
		return time.Time{}, false
	}
	var m lastRunMarker
	if err := json.Unmarshal(data, &m); err != nil {
		utils.Warn("scheduler", fmt.Sprintf("readLastRun: parse %s: %v", path, err))
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, m.LastRunUtc)
	if err != nil {
		utils.Warn("scheduler", fmt.Sprintf("readLastRun: bad timestamp %q in %s: %v", m.LastRunUtc, path, err))
		return time.Time{}, false
	}
	return ts, true
}

// markerPath computes the on-disk path for a marker. Uses the host
// name + job id as a stable key, with non-filesystem-safe characters
// replaced. Collisions across hosts with identical names are
// possible; for now we accept that — host names come from the
// extension's manifest and are conventionally unique.
func (s *Scheduler) markerPath(h *extension.Host, job extension.ScheduleJob) string {
	safe := safeName(h.Name()) + "_" + safeName(job.JobID) + ".json"
	return filepath.Join(s.persistDir, safe)
}

// safeName replaces characters that are awkward in filenames with
// '_'. Conservative — we accept ASCII alnum, dash, underscore, and
// period; everything else collapses to '_'.
func safeName(s string) string {
	if s == "" {
		return "unnamed"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
