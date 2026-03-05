package process

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/blucin/lazyproc/internal/config"
)

// State represents the lifecycle state of a managed process.
type State int

const (
	StateStopped    State = iota // not yet started or explicitly stopped
	StateStarting                // cmd.Start() called, waiting for readiness
	StateRunning                 // running, no ready_when defined (or pattern not yet matched)
	StateReady                   // ready_when pattern matched
	StateCrashed                 // exited with non-zero status or unexpected exit
	StateRestarting              // being restarted after a crash
)

// String returns a human-readable label for the state.
func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateReady:
		return "ready"
	case StateCrashed:
		return "crashed"
	case StateRestarting:
		return "restarting"
	default:
		return "unknown"
	}
}

// OutputLine is a single captured line from a process pipe.
type OutputLine struct {
	Text      string
	Timestamp time.Time
}

// StateChangeFunc is called whenever a process transitions to a new state.
// Implementations must be non-blocking (e.g. send on a buffered channel).
type StateChangeFunc func(id string, state State)

// OutputFunc is called for each line captured from stdout/stderr.
// Implementations must be non-blocking.
type OutputFunc func(id string, line OutputLine)

// Process represents a single managed child process.
type Process struct {
	mu sync.Mutex

	// Immutable identity & config set at construction time.
	ID     string
	cfg    config.Process
	shell  string
	logCap int

	// Mutable runtime state (guarded by mu).
	state        State
	cmd          *exec.Cmd
	restartCount int

	// Ring buffer holding captured output lines.
	Output *RingBuffer

	// Callbacks – set once before any Start() call.
	onStateChange StateChangeFunc
	onOutput      OutputFunc
}

// NewProcess constructs a Process from a config entry.
//
//   - id         unique name from the config map key
//   - cfg        per-process config block
//   - shell      shell binary (from global settings, e.g. "/bin/sh")
//   - logCap     ring buffer size (from global settings)
//   - onState    state-change callback (may be nil)
//   - onOutput   per-line output callback (may be nil)
func NewProcess(
	id string,
	cfg config.Process,
	shell string,
	logCap int,
	onState StateChangeFunc,
	onOutput OutputFunc,
) *Process {
	return &Process{
		ID:            id,
		cfg:           cfg,
		shell:         shell,
		logCap:        logCap,
		state:         StateStopped,
		Output:        NewRingBuffer(logCap),
		onStateChange: onState,
		onOutput:      onOutput,
	}
}

// State returns the current process state (thread-safe).
func (p *Process) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// RestartCount returns how many times the process has been restarted.
func (p *Process) RestartCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.restartCount
}

// setState transitions to newState and fires the callback.
// Must be called with p.mu held.
func (p *Process) setState(newState State) {
	p.state = newState
	if p.onStateChange != nil {
		p.onStateChange(p.ID, newState)
	}
}

// Start spawns the process. It is safe to call from any goroutine.
// Returns an error if the process is already running or cannot be started.
func (p *Process) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch p.state {
	case StateStarting, StateRunning, StateReady, StateRestarting:
		return fmt.Errorf("process %q is already running (state: %s)", p.ID, p.state)
	}

	return p.start()
}

// start does the actual spawn work. Must be called with p.mu held.
func (p *Process) start() error {
	cwd := p.cfg.Cwd

	//nolint:gosec // cmd is user-supplied from config
	cmd := exec.Command(p.shell, "-c", p.cfg.Cmd)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("process %q: attaching stdout: %w", p.ID, err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("process %q: attaching stderr: %w", p.ID, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("process %q: starting: %w", p.ID, err)
	}

	p.cmd = cmd
	p.setState(StateStarting)

	// Determine the initial running state we'll transition to once the
	// readiness pattern is irrelevant (no pattern → go straight to Running).
	hasReadyPattern := p.cfg.ReadyWhen.Stdout != ""

	// Start one reader goroutine per pipe; wait for both before watching exit.
	var wg sync.WaitGroup
	wg.Add(2)

	go p.readPipe(stdoutPipe, hasReadyPattern, &wg)
	go p.readPipe(stderrPipe, false /* stderr never triggers ready */, &wg)

	// Watcher: wait for the command to exit, then update state accordingly.
	go p.watch(cmd, &wg)

	return nil
}

// readPipe reads lines from r, pushes them into the ring buffer, fires the
// output callback, and (when checkReady is true) checks the readiness pattern.
func (p *Process) readPipe(r io.Reader, checkReady bool, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		text := scanner.Text()
		line := OutputLine{Text: text, Timestamp: time.Now()}

		p.Output.Push(text)

		if p.onOutput != nil {
			p.onOutput(p.ID, line)
		}

		if checkReady {
			p.mu.Lock()
			pattern := p.cfg.ReadyWhen.Stdout
			alreadyReady := p.state == StateReady
			p.mu.Unlock()

			if !alreadyReady && pattern != "" && matchPattern(pattern, text) {
				p.mu.Lock()
				if p.state == StateStarting || p.state == StateRunning {
					p.setState(StateReady)
				}
				p.mu.Unlock()
			}
		}
	}

	// Transition to Running if we've been in Starting and no ready pattern
	// has fired (and the pipe hasn't ended due to a crash yet).
	p.mu.Lock()
	if p.state == StateStarting {
		p.setState(StateRunning)
	}
	p.mu.Unlock()
}

// watch waits for the process to exit and transitions state accordingly.
// It is called after both pipe readers have completed.
func (p *Process) watch(cmd *exec.Cmd, wg *sync.WaitGroup) {
	// Wait for both pipe readers to drain before calling cmd.Wait so we
	// don't race on the underlying pipe file descriptors.
	wg.Wait()
	_ = cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Only mark as crashed if we were actually running; if we got a Stop()
	// call the state will already be Stopped.
	switch p.state {
	case StateStarting, StateRunning, StateReady, StateRestarting:
		p.setState(StateCrashed)
	}
}

// Stop sends SIGTERM to the process group, waits up to graceTimeout, then
// sends SIGKILL to any survivors.
func (p *Process) Stop(graceTimeout time.Duration) error {
	p.mu.Lock()

	switch p.state {
	case StateStopped, StateCrashed:
		p.mu.Unlock()
		return nil
	}

	cmd := p.cmd
	p.setState(StateStopped)
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	pgid := -cmd.Process.Pid // negative pid → send to whole process group

	// SIGTERM first.
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return fmt.Errorf("process %q: SIGTERM: %w", p.ID, err)
	}

	// Wait for voluntary exit in a goroutine; send SIGKILL after the grace period.
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Exited cleanly within the grace period.
	case <-time.After(graceTimeout):
		_ = syscall.Kill(pgid, syscall.SIGKILL)
		<-done
	}

	return nil
}

// Restart stops the process (if running) and starts it again.
func (p *Process) Restart(graceTimeout time.Duration) error {
	if err := p.Stop(graceTimeout); err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.restartCount++
	p.setState(StateRestarting)
	return p.start()
}

// ClearOutput empties the output ring buffer.
func (p *Process) ClearOutput() {
	p.Output.Clear()
}
