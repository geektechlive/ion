package backend

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
)

// TestCliResumeArgs pins the precise resume mechanism: the CLI backend
// resumes ONLY with a captured claude-native session UUID
// (RunOptions.CliResumeSessionID), never with Ion's conversation id
// (RunOptions.SessionID).
func TestCliResumeArgs(t *testing.T) {
	cases := []struct {
		name string
		opts types.RunOptions
		want []string
	}{
		{
			name: "first run: no captured UUID -> omit --resume",
			opts: types.RunOptions{},
			want: nil,
		},
		{
			name: "subsequent run: captured UUID -> --resume <uuid>",
			opts: types.RunOptions{CliResumeSessionID: "11111111-2222-3333-4444-555555555555"},
			want: []string{"--resume", "11111111-2222-3333-4444-555555555555"},
		},
		{
			name: "Ion SessionID set but no claude UUID -> still no --resume",
			opts: types.RunOptions{SessionID: "1781483744990-37463b20c27b"},
			want: nil,
		},
		{
			name: "both set -> resume uses the claude UUID, ignores Ion SessionID",
			opts: types.RunOptions{
				SessionID:          "1781483744990-37463b20c27b",
				CliResumeSessionID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			},
			want: []string{"--resume", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cliResumeArgs(tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("cliResumeArgs(%+v) = %v, want %v", tc.opts, got, tc.want)
			}
		})
	}
}

// TestRedactCliArgs pins the diagnostic argv redaction used in the enriched
// "exited with code N, no stderr" error path: prompt-bearing flag values
// (--system-prompt, --append-system-prompt) are masked as "<redacted len=N>"
// while structural argv (model, permission mode, allowlists) is preserved so
// an operator can see the shape of the failing invocation. The user prompt is
// never in argv (it goes over stdin), so no prompt flag needs masking here.
func TestRedactCliArgs(t *testing.T) {
	in := []string{
		"-p",
		"--model", "claude-sonnet-4-6",
		"--permission-mode", "bypassPermissions",
		"--system-prompt", "you are a secret persona with private text",
		"--append-system-prompt", "extra",
		"--allowedTools", "Read,Glob,Grep",
	}
	got := redactCliArgs(in)

	if strings.Contains(got, "secret persona") || strings.Contains(got, "private text") {
		t.Fatalf("redactCliArgs leaked system-prompt content: %q", got)
	}
	if strings.Contains(got, " extra") {
		t.Fatalf("redactCliArgs leaked append-system-prompt content: %q", got)
	}
	if !strings.Contains(got, "claude-sonnet-4-6") {
		t.Errorf("redactCliArgs dropped the model (structural arg must survive): %q", got)
	}
	if !strings.Contains(got, "bypassPermissions") || !strings.Contains(got, "Read,Glob,Grep") {
		t.Errorf("redactCliArgs dropped a structural arg: %q", got)
	}
	// The redaction marker carries the masked length, not the content.
	if want := "<redacted len=42>"; !strings.Contains(got, want) {
		t.Errorf("redactCliArgs = %q, want it to contain %q", got, want)
	}
	if want := "<redacted len=5>"; !strings.Contains(got, want) {
		t.Errorf("redactCliArgs = %q, want it to contain %q for --append-system-prompt", got, want)
	}
}

// TestRedactCliArgs_TrailingFlagNoValue guards the boundary case where a
// prompt-bearing flag is the last token (no value follows): it must not panic
// and must emit the bare flag.
func TestRedactCliArgs_TrailingFlagNoValue(t *testing.T) {
	got := redactCliArgs([]string{"-p", "--system-prompt"})
	if got != "-p --system-prompt" {
		t.Fatalf("redactCliArgs trailing flag = %q, want %q", got, "-p --system-prompt")
	}
}

// TestBareExitDiagnostic pins the enriched error emitted when the CLI exits
// non-zero with NO stderr captured (the previously-undiagnosable
// "exited with code 1" case). It must name where an operator should look:
// exit code, that the process started, the binary, cwd, model, and a redacted
// argv. Reverting the enrichment to a bare "exited with code N" string fails
// this test -- it pins the fix, not just the redaction helper it calls.
func TestBareExitDiagnostic(t *testing.T) {
	msg := bareExitDiagnostic(
		1, 4242, 1500*time.Millisecond,
		"/opt/homebrew/bin/claude", "/work/dir", "claude-sonnet-4-6",
		[]string{"-p", "--model", "claude-sonnet-4-6", "--system-prompt", "secret persona text"},
	)

	for _, want := range []string{
		"code 1",
		"no stderr",
		"pid=4242",
		"1.5s",
		"/opt/homebrew/bin/claude",
		"/work/dir",
		"claude-sonnet-4-6",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("bareExitDiagnostic missing %q; got %q", want, msg)
		}
	}
	if strings.Contains(msg, "secret persona") {
		t.Errorf("bareExitDiagnostic leaked system-prompt content: %q", msg)
	}
}

// TestBareExitDiagnostic_InheritedCwd pins that an empty working directory
// (the process inherits the engine's cwd) renders as "<inherited>" rather than
// an empty token that reads as missing data.
func TestBareExitDiagnostic_InheritedCwd(t *testing.T) {
	msg := bareExitDiagnostic(1, 1, time.Second, "/bin/claude", "", "m", nil)
	if !strings.Contains(msg, "cwd=<inherited>") {
		t.Errorf("empty cwd should render as <inherited>; got %q", msg)
	}
}

// TestCliSettingSourcesArgs pins the three-way mapping from
// RunOptions.CliSettingSources onto the child subprocess's argv.
//
// The load-bearing case is the middle one. CliSettingSources is a *[]string
// rather than a []string precisely so that "load no settings scope" (non-nil,
// empty) is expressible distinctly from "unset" (nil). Collapse the two and the
// feature silently becomes a no-op: the flag would never be emitted, the child
// would keep loading Claude's whole settings chain, and nothing would fail
// loudly enough to notice.
//
// The single-element `--setting-sources=<value>` form is also pinned
// deliberately. exec.Command passes argv verbatim with no shell in between, so
// the two-element {"--setting-sources", ""} form risks a flag parser reading the
// empty value as absent and consuming the following flag as the value instead.
func TestCliSettingSourcesArgs(t *testing.T) {
	empty := []string{}
	single := []string{"project"}
	multi := []string{"user", "project", "local"}

	cases := []struct {
		name string
		opts types.RunOptions
		want []string
	}{
		{
			name: "unset (nil): omit the flag, claude does its default discovery",
			opts: types.RunOptions{},
			want: nil,
		},
		{
			name: "explicitly empty: emit --setting-sources= so claude loads nothing",
			opts: types.RunOptions{CliSettingSources: &empty},
			want: []string{"--setting-sources="},
		},
		{
			name: "single scope: only that scope loads",
			opts: types.RunOptions{CliSettingSources: &single},
			want: []string{"--setting-sources=project"},
		},
		{
			name: "multiple scopes: comma-joined into one argv element",
			opts: types.RunOptions{CliSettingSources: &multi},
			want: []string{"--setting-sources=user,project,local"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cliSettingSourcesArgs(tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("cliSettingSourcesArgs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestCliSettingSourcesArgsDistinguishesEmptyFromUnset states the contract the
// pointer exists to protect, as a standalone assertion rather than a row in the
// table above: an explicitly-empty slice and an absent value must NOT produce
// the same argv. A future refactor to a plain []string would compile, would pass
// a naive "does it join correctly" test, and would break exactly here.
func TestCliSettingSourcesArgsDistinguishesEmptyFromUnset(t *testing.T) {
	empty := []string{}

	unsetArgs := cliSettingSourcesArgs(types.RunOptions{})
	emptyArgs := cliSettingSourcesArgs(types.RunOptions{CliSettingSources: &empty})

	if reflect.DeepEqual(unsetArgs, emptyArgs) {
		t.Fatalf("unset and explicitly-empty produced identical argv %#v -- the "+
			"'load nothing' instruction is indistinguishable from 'not specified', "+
			"which makes the feature a silent no-op", unsetArgs)
	}
	if len(unsetArgs) != 0 {
		t.Errorf("unset produced %#v, want no argv (current behavior must be unchanged)", unsetArgs)
	}
	if len(emptyArgs) != 1 || !strings.HasPrefix(emptyArgs[0], "--setting-sources=") {
		t.Errorf("explicitly-empty produced %#v, want a single --setting-sources= element", emptyArgs)
	}
}
