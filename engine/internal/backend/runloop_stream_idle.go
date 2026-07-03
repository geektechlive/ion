package backend

import (
	"github.com/dsswift/ion/engine/internal/providers"
)

// runloop_stream_idle.go — installs the provider stream-idle deadline for a
// run from its resolved TimeoutsConfig.
//
// The providers package reads the deadline via a package-level atomic
// (SetStreamIdleTimeout, mirroring runProgressWatchdogTickNanos), so the
// streaming hot path reads a plain int64 rather than threading config through
// every provider's Stream signature — which would be a wire/interface contract
// change. In production every run shares the engine's single TimeoutsConfig, so
// the global is effectively stable; it is re-asserted per run so a config
// reload takes effect on the next run without a process restart.
//
// Extracted to its own file (rather than inlined in the allowlisted-near-cap
// runloop.go) per the file-organization rule that new code goes in a new file.
func installStreamIdleTimeout(run *activeRun) {
	if run == nil || run.cfg == nil || run.cfg.Timeouts == nil {
		// No per-run timeouts config: leave whatever default the providers
		// package already has (its own 90s compiled default). Do not reset —
		// a prior run on this process may have installed a valid value.
		return
	}
	if d, enabled := run.cfg.Timeouts.StreamIdle(); enabled {
		providers.SetStreamIdleTimeout(d)
	} else {
		providers.SetStreamIdleTimeout(-1) // negative disables the deadline
	}
}
