// Package scheduling implements the engine-internal scheduler that
// fires registered ScheduleJob declarations on their configured
// cadence.
//
// Architecture mirrors the webhook server (see engine/internal/webhooks):
//
//   - Scheduler is a per-session-manager singleton. Hosts are added /
//     removed dynamically as extensions load; the scheduler reads each
//     host's asyncreg registry to enumerate jobs.
//   - One tick loop ticks every second (TickInterval). On each tick,
//     the loop walks every host's schedule registry, fires every job
//     whose next-run is ≤ now, and updates the in-memory next-run map.
//   - Per-fire arbitration: an in-process sync.Map prevents two
//     concurrent fires of the same (host, jobId). The plan calls for
//     cross-subprocess flock arbitration; that's deferred to a future
//     iteration since the engine currently runs as a single process.
//     (When a daemon-mode engine sits behind multiple desktop clients,
//     they share a process, so in-process arbitration is sufficient.)
//   - Last-run markers are persisted to disk under ~/.ion/scheduler so
//     daily/weekly catch-up survives engine restarts.
//
// The scheduler is the trigger source only — every downstream concern
// (session resolution, ctx injection, handler dispatch) is shared with
// the webhook server via host.FireAsync.
package scheduling

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dsswift/ion/engine/internal/asyncreg"
	"github.com/dsswift/ion/engine/internal/extension"
	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// TickInterval is the scheduler's main loop cadence. 1s is the
// engineered floor for daily/weekly resolution (any finer would
// over-trigger near minute boundaries) and the documented minimum
// IntervalMs for interval jobs.
const TickInterval = time.Second

// DefaultFireTimeout caps a schedule handler invocation. Generous so
// extensions can dispatch agents inside the handler. Interval jobs may
// override via ScheduleJob.TimeoutMs.
const DefaultFireTimeout = 60 * time.Second

// CatchUpStagger is the additional delay the scheduler applies to
// missed daily/weekly jobs when a restart triggers catch-up. Spreads
// fires across a 30s window so a restart with 10 jobs doesn't fire
// them all at once.
const CatchUpStagger = 30 * time.Second

// SessionResolver matches the webhook server's resolver: given a host,
// return a fresh extension.Context for the bound session. Wired by the
// session manager.
type SessionResolver func(host *extension.Host) (*extension.Context, error)

// Scheduler is the engine-internal job runner. New() constructs but
// does not start; Start() launches the tick loop; Stop() signals
// shutdown and waits for the loop to exit.
type Scheduler struct {
	cfg Config

	mu       sync.RWMutex
	hosts    []*extension.Host
	emit     func(types.EngineEvent)
	resolve  SessionResolver
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	nextRun  map[hostJobKey]time.Time
	inFlight sync.Map // hostJobKey -> struct{}

	// persistDir is the directory under which last-run markers are
	// persisted. Empty means no persistence (tests / catch-up
	// disabled).
	persistDir string

	// nowFn is a test-injectable clock. nil means real time.Now.
	nowFn func() time.Time
}

// hostJobKey scopes a job by its owning host pointer plus the job id.
// Two hosts can use the same job id without interfering.
type hostJobKey struct {
	host *extension.Host
	id   string
}

// extensionJobKey groups jobs by extension name + job ID for
// concurrency coordination. All hosts of the same extension with the
// same job ID share one extensionJobKey.
type extensionJobKey struct {
	name string // host.Name()
	id   string // job.JobID
}

// hostJobEntry pairs a host with a job for the group-then-fire pass.
type hostJobEntry struct {
	host *extension.Host
	job  extension.ScheduleJob
}

// Config holds the engine-config-controlled defaults for the
// scheduler. All fields zero-valued to inherit engine defaults.
type Config struct {
	// DefaultTz is the IANA timezone applied to daily/weekly jobs
	// whose ScheduleJob.Tz is empty. Empty inherits the system local
	// timezone.
	DefaultTz string
	// FireTimeout is the per-fire handler timeout default. Zero falls
	// back to DefaultFireTimeout. Per-job override is the job's
	// TimeoutMs.
	FireTimeout time.Duration
	// CatchUpEnabled controls whether missed daily/weekly fires fire on
	// engine startup. Nil treats as default-on.
	CatchUpEnabled *bool
	// PersistDir is the directory for last-run markers. Empty disables
	// persistence (catch-up still works on a per-process basis).
	PersistDir string
}

// New constructs a Scheduler with the given Config.
func New(cfg Config) *Scheduler {
	return &Scheduler{
		cfg:        cfg,
		nextRun:    make(map[hostJobKey]time.Time),
		persistDir: cfg.PersistDir,
	}
}

// SetEmit wires the session emitter for engine_schedule_* events.
func (s *Scheduler) SetEmit(fn func(types.EngineEvent)) {
	s.mu.Lock()
	s.emit = fn
	s.mu.Unlock()
}

// SetSessionResolver wires the per-fire session-resolution callback.
func (s *Scheduler) SetSessionResolver(fn SessionResolver) {
	s.mu.Lock()
	s.resolve = fn
	s.mu.Unlock()
}

// AddHost adds a host whose schedule registry will be polled by the
// tick loop. Idempotent.
func (s *Scheduler) AddHost(h *extension.Host) {
	if h == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.hosts {
		if existing == h {
			return
		}
	}
	s.hosts = append(s.hosts, h)
	utils.Debug("scheduler", fmt.Sprintf("AddHost: ext=%s total_hosts=%d", h.Name(), len(s.hosts)))
}

// RemoveHost removes a host from the schedule pool. In-flight fires
// for that host continue to completion; new fires won't dispatch.
func (s *Scheduler) RemoveHost(h *extension.Host) {
	if h == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.hosts {
		if existing == h {
			s.hosts = append(s.hosts[:i], s.hosts[i+1:]...)
			// Drop next-run entries for jobs we're no longer tracking.
			for k := range s.nextRun {
				if k.host == h {
					delete(s.nextRun, k)
				}
			}
			utils.Debug("scheduler", fmt.Sprintf("RemoveHost: ext=%s remaining_hosts=%d", h.Name(), len(s.hosts)))
			return
		}
	}
}

// Start launches the tick loop. Idempotent — calling Start when
// already running is a no-op.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	utils.Log("scheduler", fmt.Sprintf("Start: tick=%s fire-timeout=%s default-tz=%s persist=%s",
		TickInterval, s.fireTimeout(), s.defaultTz(), s.persistDir))

	go s.runLoop()
}

// Stop signals the tick loop to exit and waits for it to finish.
// Idempotent.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.running = false
	s.mu.Unlock()

	close(stopCh)
	<-doneCh
	utils.Log("scheduler", "Stop: tick loop exited")
}

// runLoop is the scheduler's main goroutine. Ticks once per
// TickInterval, walks every host's schedule registry, fires every
// job whose next-run is ≤ now, and updates the in-memory next-run.
func (s *Scheduler) runLoop() {
	defer close(s.doneCh)
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()
	// Initial pass on startup so a freshly-registered job whose first
	// run is ≤ now fires without waiting a full tick.
	s.tickOnce()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tickOnce()
		}
	}
}

// tickOnce performs a single pass over every registered job. Jobs are
// grouped by (extensionName, jobID) for concurrency coordination.
// In "single" mode (the default), only the first alive host in each
// group fires. In "all" mode, every host fires independently.
// Public for tests so they can step the scheduler deterministically.
func (s *Scheduler) tickOnce() {
	now := s.now()
	s.mu.RLock()
	hosts := append([]*extension.Host(nil), s.hosts...)
	resolve := s.resolve
	s.mu.RUnlock()

	// Group jobs by (extensionName, jobID) for concurrency coordination.
	groups := make(map[extensionJobKey][]hostJobEntry)
	for _, h := range hosts {
		decls := h.AsyncRegistry().List(asyncreg.KindSchedule)
		for _, d := range decls {
			job, ok := d.(extension.ScheduleJob)
			if !ok {
				continue
			}
			key := extensionJobKey{name: h.Name(), id: job.JobID}
			groups[key] = append(groups[key], hostJobEntry{host: h, job: job})
		}
	}

	// Fire each group according to its concurrency mode.
	for _, entries := range groups {
		if len(entries) == 0 {
			continue
		}
		concurrency := entries[0].job.Concurrency
		if concurrency == "all" {
			// All mode: fire on every host (opt-in behavior).
			for _, e := range entries {
				s.maybeFire(e.host, e.job, now, resolve)
			}
		} else {
			// Single mode (default): fire on the first alive host only.
			for _, e := range entries {
				if !e.host.Dead() {
					s.maybeFire(e.host, e.job, now, resolve)
					break
				}
			}
		}
	}
}

// maybeFire decides whether a job's next-run has elapsed and, if so,
// schedules a goroutine to dispatch the fire. Returns immediately if
// not yet due, currently in-flight, or just registered (next-run not
// yet computed).
func (s *Scheduler) maybeFire(h *extension.Host, job extension.ScheduleJob, now time.Time, resolve SessionResolver) {
	key := hostJobKey{host: h, id: job.JobID}
	s.mu.RLock()
	next, computed := s.nextRun[key]
	s.mu.RUnlock()
	if !computed {
		// First sighting — compute next-run, possibly run catch-up,
		// and store. Don't fire on this tick; the next tick will pick
		// it up if applicable.
		s.bootstrapNextRun(h, job, now)
		return
	}
	if now.Before(next) {
		return
	}
	if _, busy := s.inFlight.LoadOrStore(key, struct{}{}); busy {
		// A previous fire is still running; skip this tick to avoid
		// overlap. Log so the operator sees the overlap.
		utils.Log("scheduler", fmt.Sprintf("maybeFire: skip ext=%s id=%q (previous fire in flight)", h.Name(), job.JobID))
		return
	}
	if resolve == nil {
		s.inFlight.Delete(key)
		s.emitScheduleSkipped(job, "no_resolver")
		utils.Error("scheduler", fmt.Sprintf("maybeFire: ext=%s id=%q no resolver wired", h.Name(), job.JobID))
		s.advanceNextRun(key, job, now)
		return
	}
	go s.fireJob(h, job, key, resolve)
}

// fireJob runs the handler invocation for a single tick. Blocks until
// the subprocess responds or the timeout elapses; releases the
// in-flight slot before returning.
func (s *Scheduler) fireJob(h *extension.Host, job extension.ScheduleJob, key hostJobKey, resolve SessionResolver) {
	defer s.inFlight.Delete(key)
	now := s.now()
	// Advance next-run BEFORE the fire so a slow handler doesn't
	// cause overlapping fires on the next tick. The in-flight guard
	// also prevents overlap; this is the second layer.
	s.advanceNextRun(key, job, now)

	ctx, err := resolve(h)
	if err != nil || ctx == nil {
		s.emitScheduleSkipped(job, "no_session")
		utils.Log("scheduler", fmt.Sprintf("fireJob: ext=%s id=%q session resolve failed: %v", h.Name(), job.JobID, err))
		return
	}

	// Optional enable-predicate callback.
	if job.EnabledRefName != "" {
		enabled, err := s.resolveEnabledPredicate(h, job)
		if err != nil {
			// A predicate that errors (rather than returning a clean
			// enabled/disabled bool) means the enable-check itself is
			// broken -- log at ERROR so log scanners that key on error
			// level surface it. The job is skipped this tick.
			utils.Error("scheduler", fmt.Sprintf("fireJob: ext=%s id=%q enabled-predicate failed: %v", h.Name(), job.JobID, err))
			// Treat predicate failure as "skipped, reason=predicate_error".
			s.emitScheduleSkipped(job, "predicate_error")
			return
		}
		if !enabled {
			s.emitScheduleSkipped(job, "disabled")
			utils.Debug("scheduler", fmt.Sprintf("fireJob: ext=%s id=%q skipped (disabled)", h.Name(), job.JobID))
			return
		}
	}

	timeout := s.fireTimeoutForJob(job)
	utils.Log("scheduler", fmt.Sprintf("fireJob: ext=%s id=%q kind=%s timeout=%s", h.Name(), job.JobID, job.Kind, timeout))
	startTs := s.now()
	payload := map[string]interface{}{
		"firedAt": startTs.UTC().Format(time.RFC3339),
	}
	_, err = h.FireAsync(asyncreg.KindSchedule, job.JobID, ctx, payload, timeout)
	elapsed := s.now().Sub(startTs)
	if err != nil {
		s.emitScheduleFailed(job, err.Error(), elapsed)
		// A fire failure means the handler invocation itself failed
		// (subprocess error, timeout, transport fault) -- log at ERROR
		// so it is not lost in the INFO stream that log scanners skip.
		utils.Error("scheduler", fmt.Sprintf("fireJob: ext=%s id=%q handler error: %v (elapsed=%s)", h.Name(), job.JobID, err, elapsed))
		return
	}
	s.recordLastRun(h, job, startTs)
	s.emitScheduleFired(job, elapsed)
	utils.Log("scheduler", fmt.Sprintf("fireJob: ext=%s id=%q completed elapsed=%s", h.Name(), job.JobID, elapsed))
}

// resolveEnabledPredicate calls back into the subprocess to evaluate
// the job's `() => bool` enabled predicate. Returns the predicate
// result, or an error if the RPC fails. Mirrors Host.ResolveToken's
// shape — short timeout, tolerant decoder.
func (s *Scheduler) resolveEnabledPredicate(h *extension.Host, job extension.ScheduleJob) (bool, error) {
	raw, err := h.ResolvePredicate(job.EnabledRefName)
	if err != nil {
		return false, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return true, nil
	}
	var asObj struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(raw, &asObj); err == nil {
		return asObj.Enabled, nil
	}
	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return asBool, nil
	}
	return false, fmt.Errorf("resolveEnabledPredicate: unrecognised response: %s", string(raw))
}
