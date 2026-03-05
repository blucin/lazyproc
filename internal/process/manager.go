package process

import (
	"fmt"
	"sync"
	"time"

	"github.com/blucin/lazyproc/internal/config"
)

const defaultGraceTimeout = 5 * time.Second

// Manager owns and coordinates all managed processes.
// It is the single point of truth for process lifecycle across the application.
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*Process

	// Callbacks forwarded to each Process at construction time.
	onStateChange StateChangeFunc
	onOutput      OutputFunc

	shell  string
	logCap int
}

// NewManager constructs a Manager from the parsed config.
//
//   - cfg         the fully-parsed lazyproc config
//   - onState     called whenever any process changes state (may be nil)
//   - onOutput    called for every output line from any process (may be nil)
func NewManager(cfg *config.Config, onState StateChangeFunc, onOutput OutputFunc) *Manager {
	m := &Manager{
		processes:     make(map[string]*Process, len(cfg.Processes)),
		onStateChange: onState,
		onOutput:      onOutput,
		shell:         cfg.Settings.Shell,
		logCap:        cfg.Settings.LogLimit,
	}

	for id, pcfg := range cfg.Processes {
		m.processes[id] = NewProcess(
			id,
			pcfg,
			m.shell,
			m.logCap,
			onState,
			onOutput,
		)
	}

	return m
}

// Get returns the Process with the given id, or nil if it does not exist.
func (m *Manager) Get(id string) *Process {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.processes[id]
}

// IDs returns all known process IDs in an unspecified order.
func (m *Manager) IDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.processes))
	for id := range m.processes {
		ids = append(ids, id)
	}
	return ids
}

// Snapshot returns a point-in-time slice of all processes.
// The returned slice is safe to iterate without holding the manager lock,
// though individual Process state fields have their own locks.
func (m *Manager) Snapshot() []*Process {
	m.mu.RLock()
	defer m.mu.RUnlock()

	procs := make([]*Process, 0, len(m.processes))
	for _, p := range m.processes {
		procs = append(procs, p)
	}
	return procs
}

// StartAll starts every process that is currently stopped or crashed.
// Processes with dependencies are started after their dependencies reach
// the required state (Ready, or Running when no ready_when is configured).
// This call blocks until the full startup sequence completes or an error occurs.
func (m *Manager) StartAll() error {
	m.mu.RLock()
	procs := make(map[string]*Process, len(m.processes))
	for id, p := range m.processes {
		procs[id] = p
	}
	m.mu.RUnlock()

	return startOrdered(procs)
}

// Start starts a single process by id.
// Returns an error if the id is unknown or the process fails to start.
func (m *Manager) Start(id string) error {
	p := m.Get(id)
	if p == nil {
		return fmt.Errorf("unknown process %q", id)
	}
	return p.Start()
}

// Stop stops a single process by id using the default grace timeout.
func (m *Manager) Stop(id string) error {
	p := m.Get(id)
	if p == nil {
		return fmt.Errorf("unknown process %q", id)
	}
	return p.Stop(defaultGraceTimeout)
}

// Restart restarts a single process by id.
func (m *Manager) Restart(id string) error {
	p := m.Get(id)
	if p == nil {
		return fmt.Errorf("unknown process %q", id)
	}
	return p.Restart(defaultGraceTimeout)
}

// StopAll sends SIGTERM to every running process group concurrently, then
// waits for all of them (SIGKILL after the grace period for survivors).
func (m *Manager) StopAll() {
	procs := m.Snapshot()

	var wg sync.WaitGroup
	wg.Add(len(procs))

	for _, p := range procs {
		go func(proc *Process) {
			defer wg.Done()
			_ = proc.Stop(defaultGraceTimeout)
		}(p)
	}

	wg.Wait()
}

// startOrdered starts processes respecting depends_on ordering.
// It runs a simple iterative pass: on each iteration it starts every process
// whose dependencies are all satisfied, until nothing is left or no progress
// can be made (cycle or unsatisfiable dependency — validated at config-parse
// time, so this is a safety net).
func startOrdered(procs map[string]*Process) error {
	remaining := make(map[string]*Process, len(procs))
	for id, p := range procs {
		remaining[id] = p
	}

	const pollInterval = 100 * time.Millisecond
	const depTimeout = 60 * time.Second
	deadline := time.Now().Add(depTimeout)

	for len(remaining) > 0 {
		if time.Now().After(deadline) {
			ids := make([]string, 0, len(remaining))
			for id := range remaining {
				ids = append(ids, id)
			}
			return fmt.Errorf("dependency timeout: processes still waiting: %v", ids)
		}

		progress := false

		for id, p := range remaining {
			if depsSatisfied(p, procs) {
				st := p.State()
				if st == StateStopped || st == StateCrashed {
					if err := p.Start(); err != nil {
						return fmt.Errorf("starting process %q: %w", id, err)
					}
				}
				delete(remaining, id)
				progress = true
			}
		}

		if !progress && len(remaining) > 0 {
			// No process was ready this iteration — wait for a dependency
			// to reach the required state before trying again.
			time.Sleep(pollInterval)
		}
	}

	return nil
}

// depsSatisfied reports whether all of p's depends_on requirements are met.
// A dependency is satisfied when it is in Ready state, or in Running state
// when it has no ready_when pattern defined (meaning it will never become Ready).
func depsSatisfied(p *Process, all map[string]*Process) bool {
	for _, depID := range p.cfg.DependsOn {
		dep, ok := all[depID]
		if !ok {
			// Unknown dep — config validation should have caught this; skip.
			continue
		}

		st := dep.State()
		hasReadyPattern := dep.cfg.ReadyWhen.Stdout != ""

		switch {
		case st == StateReady:
			// Always satisfied.
		case st == StateRunning && !hasReadyPattern:
			// No ready pattern; Running is as good as it gets.
		default:
			return false
		}
	}
	return true
}
