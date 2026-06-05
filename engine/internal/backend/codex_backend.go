package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dsswift/ion/engine/internal/types"
	"github.com/dsswift/ion/engine/internal/utils"
)

// codex JSONL event shapes (stdout from `codex exec --json`).

type codexThreadStarted struct {
	ThreadID string `json:"thread_id"`
}

type codexItem struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexItemCompleted struct {
	Item codexItem `json:"item"`
}

type codexTurnCompleted struct {
	Usage codexUsage `json:"usage"`
}

type codexUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type codexTurnFailed struct {
	Error codexErrorDetail `json:"error"`
}

type codexErrorEvent struct {
	Message string `json:"message"`
}

type codexErrorDetail struct {
	Message string `json:"message"`
}

// codexRun tracks an active Codex CLI process.
type codexRun struct {
	requestID string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stderr    *ringBuffer
}

// CodexCliBackend implements RunBackend by spawning the OpenAI Codex CLI
// (`codex exec --json`) and parsing its JSONL output.
//
// Unlike CliBackend (Claude), Codex handles all tool calls internally — ion
// does not inject tool results via stdin. WriteToStdin is a deliberate no-op.
//
// System prompts from the before_prompt hook are prepended to the user prompt
// text because Codex has no --system-prompt flag.
type CodexCliBackend struct {
	mu         sync.Mutex
	activeRuns map[string]*codexRun

	onNormalized func(string, types.NormalizedEvent)
	onExit       func(string, *int, *string, string)
	onError      func(string, error)
}

// NewCodexCliBackend creates a CodexCliBackend ready for use.
func NewCodexCliBackend() *CodexCliBackend {
	return &CodexCliBackend{
		activeRuns: make(map[string]*codexRun),
	}
}

// findCodexBinary locates the codex CLI binary.
// Search order: ~/.local/bin, /usr/local/bin, /opt/homebrew/bin, $PATH, login shell.
func findCodexBinary() (string, error) {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "codex"),
		"/usr/local/bin/codex",
		"/opt/homebrew/bin/codex",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p, nil
	}
	if runtime.GOOS != "windows" {
		for _, shell := range []string{"zsh", "bash"} {
			if shellPath, err := exec.LookPath(shell); err == nil {
				out, err := exec.Command(shellPath, "-l", "-c", "which codex 2>/dev/null").Output()
				if err == nil {
					if p := strings.TrimSpace(string(out)); p != "" {
						if _, err := os.Stat(p); err == nil {
							return p, nil
						}
					}
				}
			}
		}
	}
	return "", fmt.Errorf("codex CLI not found: checked ~/.local/bin/codex, /usr/local/bin/codex, /opt/homebrew/bin/codex, $PATH, and login shell")
}

// OnNormalized registers the callback for normalized events.
func (b *CodexCliBackend) OnNormalized(fn func(string, types.NormalizedEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onNormalized = fn
}

// OnExit registers the callback for run exit events.
func (b *CodexCliBackend) OnExit(fn func(string, *int, *string, string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onExit = fn
}

// OnError registers the callback for run errors.
func (b *CodexCliBackend) OnError(fn func(string, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onError = fn
}

// IsRunning reports whether a run is currently active.
func (b *CodexCliBackend) IsRunning(requestID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.activeRuns[requestID]
	return ok
}

// Cancel stops a running Codex process. Sends SIGINT first, then escalates
// to SIGKILL after 5 seconds if the process has not exited.
func (b *CodexCliBackend) Cancel(requestID string) bool {
	b.mu.Lock()
	run, ok := b.activeRuns[requestID]
	b.mu.Unlock()
	if !ok {
		return false
	}

	proc := run.cmd.Process
	if proc == nil {
		run.cancel()
		return true
	}

	if runtime.GOOS == "windows" {
		_ = proc.Kill()
		run.cancel()
		return true
	}

	if err := proc.Signal(syscall.SIGINT); err != nil {
		utils.Log("CodexCli", "SIGINT failed, killing: "+err.Error())
		_ = proc.Kill()
		run.cancel()
		return true
	}

	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		<-timer.C
		b.mu.Lock()
		_, stillActive := b.activeRuns[requestID]
		b.mu.Unlock()
		if stillActive {
			utils.Log("CodexCli", "process did not exit after SIGINT, sending SIGKILL: "+requestID)
			_ = proc.Signal(syscall.SIGKILL)
			run.cancel()
		}
	}()

	return true
}

// StartRun spawns a Codex CLI process and streams its JSONL output.
func (b *CodexCliBackend) StartRun(requestID string, options types.RunOptions) {
	ctx, cancel := context.WithCancel(context.Background())
	run := &codexRun{
		requestID: requestID,
		cancel:    cancel,
		stderr:    newRingBuffer(100),
	}
	b.mu.Lock()
	b.activeRuns[requestID] = run
	b.mu.Unlock()
	go b.runProcess(ctx, run, options)
}

// WriteToStdin is a no-op for CodexCliBackend. Codex handles all tool calls
// internally; ion does not inject tool results via stdin on this backend.
func (b *CodexCliBackend) WriteToStdin(requestID string, _ interface{}) error {
	utils.Log("CodexCli", fmt.Sprintf("WriteToStdin: requestID=%s — no-op (Codex handles tools internally)", requestID))
	return nil
}

// FlushConversations is a no-op; Codex manages its own session persistence.
func (b *CodexCliBackend) FlushConversations() {}

// runProcess manages the Codex CLI process lifecycle.
func (b *CodexCliBackend) runProcess(ctx context.Context, run *codexRun, opts types.RunOptions) {
	defer func() {
		time.AfterFunc(5*time.Second, func() {
			b.removeRun(run.requestID)
		})
	}()

	codexPath, err := findCodexBinary()
	if err != nil {
		utils.Error("CodexCli", "codex binary not found: "+err.Error())
		b.emitError(run.requestID, err)
		b.emitExit(run.requestID, intPtr(1), nil, "")
		return
	}

	// Build the prompt text. Codex has no --system-prompt flag, so inject
	// system context from the before_prompt hook by prepending it.
	promptText := opts.Prompt
	if opts.SystemPrompt != "" {
		promptText = "[Instructions]\n" + opts.SystemPrompt + "\n\n" + promptText
	} else if opts.AppendSystemPrompt != "" {
		promptText = "[Instructions]\n" + opts.AppendSystemPrompt + "\n\n" + promptText
	}

	args := []string{"exec", "--json", "--skip-git-repo-check"}

	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}

	// Session resume: if a prior thread_id is supplied, ask Codex to resume it.
	// The thread_id is populated from the previous run's thread.started event
	// and passed back via OnExit → session manager → opts.SessionID.
	if opts.SessionID != "" {
		args = append(args, "resume", opts.SessionID)
	}

	args = append(args, promptText)

	utils.Log("CodexCli", fmt.Sprintf("spawning: %s exec --json --skip-git-repo-check [model=%s sessionID=%s] requestID=%s",
		codexPath, opts.Model, opts.SessionID, run.requestID))

	cmd := exec.CommandContext(ctx, codexPath, args...)
	if opts.ProjectPath != "" {
		cmd.Dir = opts.ProjectPath
	}
	run.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		utils.Error("CodexCli", "stdout pipe failed: "+err.Error())
		b.emitError(run.requestID, fmt.Errorf("failed to create stdout pipe: %w", err))
		b.emitExit(run.requestID, intPtr(1), nil, "")
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		utils.Error("CodexCli", "stderr pipe failed: "+err.Error())
		b.emitError(run.requestID, fmt.Errorf("failed to create stderr pipe: %w", err))
		b.emitExit(run.requestID, intPtr(1), nil, "")
		return
	}

	if err := cmd.Start(); err != nil {
		utils.Error("CodexCli", "process start failed: "+err.Error())
		b.emitError(run.requestID, fmt.Errorf("failed to start codex CLI: %w", err))
		b.emitExit(run.requestID, intPtr(1), nil, "")
		return
	}

	utils.Log("CodexCli", fmt.Sprintf("process started: pid=%d requestID=%s", cmd.Process.Pid, run.requestID))

	var stderrDone sync.WaitGroup
	stderrDone.Add(1)
	go func() {
		defer stderrDone.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			run.stderr.Write(scanner.Text())
		}
	}()

	sessionID, textParts, errorMsg, usage := b.parseCodexOutput(stdout, run.requestID, opts.Model)

	stderrDone.Wait()

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	utils.Log("CodexCli", fmt.Sprintf("process exited: pid=%d code=%d requestID=%s", cmd.Process.Pid, exitCode, run.requestID))

	if errorMsg != "" || exitCode != 0 {
		if errorMsg == "" {
			stderrLines := run.stderr.Lines()
			if len(stderrLines) > 0 {
				errorMsg = fmt.Sprintf("codex CLI exited with code %d: %s", exitCode, strings.Join(stderrLines, "\n"))
			} else {
				errorMsg = fmt.Sprintf("codex CLI exited with code %d", exitCode)
			}
		}
		b.emitError(run.requestID, fmt.Errorf("%s", errorMsg))
	} else {
		result := strings.Join(textParts, "")
		b.emit(run.requestID, types.NormalizedEvent{
			Data: &types.TaskCompleteEvent{
				Result:    result,
				SessionID: sessionID,
				Usage: types.UsageData{
					InputTokens:  &usage.InputTokens,
					OutputTokens: &usage.OutputTokens,
				},
			},
		})
	}

	b.emitExit(run.requestID, &exitCode, nil, sessionID)
}

// parseCodexOutput reads the JSONL event stream from stdout and emits
// normalized events. Returns sessionID, accumulated text chunks, error message,
// and usage data from the completed run.
func (b *CodexCliBackend) parseCodexOutput(stdout interface{ Read([]byte) (int, error) }, requestID, model string) (sessionID string, textParts []string, errorMsg string, usage codexUsage) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &peek); err != nil {
			utils.Log("CodexCli", fmt.Sprintf("failed to parse JSONL line: %s requestID=%s", err.Error(), requestID))
			continue
		}

		utils.Debug("CodexCli", fmt.Sprintf("event: type=%s requestID=%s", peek.Type, requestID))

		switch peek.Type {
		case "thread.started":
			var ev codexThreadStarted
			if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.ThreadID != "" {
				sessionID = ev.ThreadID
				utils.Log("CodexCli", fmt.Sprintf("thread started: thread_id=%s requestID=%s", sessionID, requestID))
			}
			b.emit(requestID, types.NormalizedEvent{Data: &types.SessionInitEvent{
				SessionID: sessionID,
				Model:     model,
			}})

		case "item.completed":
			var ev codexItemCompleted
			if err := json.Unmarshal([]byte(line), &ev); err == nil {
				if ev.Item.Type == "agent_message" && ev.Item.Text != "" {
					textParts = append(textParts, ev.Item.Text)
					b.emit(requestID, types.NormalizedEvent{
						Data: &types.TextChunkEvent{Text: ev.Item.Text},
					})
					utils.Debug("CodexCli", fmt.Sprintf("agent_message: len=%d requestID=%s", len(ev.Item.Text), requestID))
				}
			}

		case "turn.completed":
			var ev codexTurnCompleted
			if err := json.Unmarshal([]byte(line), &ev); err == nil {
				usage = ev.Usage
			}
			utils.Log("CodexCli", fmt.Sprintf("turn completed: input_tokens=%d output_tokens=%d requestID=%s",
				usage.InputTokens, usage.OutputTokens, requestID))

		case "turn.failed":
			var ev codexTurnFailed
			if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Error.Message != "" {
				errorMsg = ev.Error.Message
			}
			utils.Error("CodexCli", fmt.Sprintf("turn failed: %s requestID=%s", errorMsg, requestID))

		case "error":
			var ev codexErrorEvent
			if err := json.Unmarshal([]byte(line), &ev); err == nil && ev.Message != "" {
				errorMsg = ev.Message
			}
			utils.Error("CodexCli", fmt.Sprintf("error event: %s requestID=%s", errorMsg, requestID))
		}
	}
	return
}

func (b *CodexCliBackend) removeRun(requestID string) {
	b.mu.Lock()
	delete(b.activeRuns, requestID)
	b.mu.Unlock()
}

func (b *CodexCliBackend) emit(runID string, event types.NormalizedEvent) {
	b.mu.Lock()
	fn := b.onNormalized
	b.mu.Unlock()
	if fn != nil {
		fn(runID, event)
	}
}

func (b *CodexCliBackend) emitExit(runID string, code *int, signal *string, sessionID string) {
	codeStr, sigStr := "nil", "nil"
	if code != nil {
		codeStr = fmt.Sprintf("%d", *code)
	}
	if signal != nil {
		sigStr = *signal
	}
	utils.Info("CodexCli", fmt.Sprintf("emitExit: runID=%s code=%s signal=%s sessionID=%s", runID, codeStr, sigStr, sessionID))
	b.mu.Lock()
	fn := b.onExit
	b.mu.Unlock()
	if fn != nil {
		fn(runID, code, signal, sessionID)
	}
}

func (b *CodexCliBackend) emitError(runID string, err error) {
	utils.Error("CodexCli", fmt.Sprintf("emitError: runID=%s err=%s", runID, err.Error()))
	b.mu.Lock()
	fn := b.onError
	b.mu.Unlock()
	if fn != nil {
		fn(runID, err)
	}
}
