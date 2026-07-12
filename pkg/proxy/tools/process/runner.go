// Package process provides generic process lifecycle management.
// This is a reusable infrastructure component for managing external processes.
package process

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
)

// RunnerConfig holds configuration for starting a process.
type RunnerConfig struct {
	// BinaryPath is the path to the executable.
	BinaryPath string

	// Args are the command-line arguments for the process.
	// Common arguments like "run", "-c" should be specified here.
	Args []string

	// ConfigFile is the path to the configuration file (optional).
	ConfigFile string

	// Stdout specifies where to pipe stdout (nil = os.Stdout).
	Stdout interface{}

	// Stderr specifies where to pipe stderr (nil = os.Stderr).
	Stderr interface{}

	// Env holds additional KEY=VALUE environment entries appended to the
	// parent process's environment. Empty slice (or nil) leaves the child
	// with the default os.Environ(). Used e.g. by the mihomo container to
	// set SAFE_PATHS without having to reimplement process launching.
	Env []string
}

// Runner provides generic external process lifecycle management.
// It can be embedded or used by any component that needs to manage external processes.
type Runner struct {
	config RunnerConfig
	cmd    *exec.Cmd
	mu     sync.Mutex
	// waitDone is closed by the current run's reaper after cmd.Wait() returns.
	waitDone chan struct{}
	// exited is set by the reaper once the process has actually exited, so
	// IsRunning/PID reflect real liveness instead of "started and not Stopped".
	exited atomic.Bool
	// stopping (per run) is set by Stop before it signals the process, so the
	// reaper can log an intentional stop at debug rather than as a crash.
	stopping *atomic.Bool
}

// NewRunner creates a new Runner with the given configuration.
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.BinaryPath == "" {
		return nil, fmt.Errorf("binary path required")
	}
	return &Runner{
		config: cfg,
	}, nil
}

// Start launches the process.
func (p *Runner) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reject only if a process is genuinely still alive. A previous process that
	// self-exited (and was reaped) must not block a fresh Start — callers use
	// IsRunning()==false to skip Stop and Start directly, so a crashed process
	// has to be restartable here.
	if p.cmd != nil && p.cmd.Process != nil && !p.exited.Load() {
		return fmt.Errorf("process already running")
	}

	// Build command
	args := p.config.Args
	if p.config.ConfigFile != "" {
		// Insert -c flag before config file path if needed
		args = append(args, "-c", p.config.ConfigFile)
	}
	cmd := exec.Command(p.config.BinaryPath, args...)

	// Set stdout/stderr
	if p.config.Stdout != nil {
		cmd.Stdout = p.config.Stdout.(interface{ Write([]byte) (int, error) })
	} else {
		cmd.Stdout = os.Stdout
	}
	if p.config.Stderr != nil {
		cmd.Stderr = p.config.Stderr.(interface{ Write([]byte) (int, error) })
	} else {
		cmd.Stderr = os.Stderr
	}

	// Append caller-supplied env on top of the inherited environment.
	// Leaving cmd.Env at nil would use os.Environ() — equivalent to "no
	// override"; explicitly appending makes it obvious what we're doing
	// and avoids dropping the parent env if a future caller sets only
	// Env without realising nil means "inherit".
	if len(p.config.Env) > 0 {
		cmd.Env = append(os.Environ(), p.config.Env...)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	p.cmd = cmd
	p.exited.Store(false)
	done := make(chan struct{})
	p.waitDone = done
	stopping := &atomic.Bool{}
	p.stopping = stopping
	name := p.config.BinaryPath // snapshot under lock (SetConfig may race otherwise)

	// The reaper is the SOLE caller of cmd.Wait() — it reaps the child (no
	// zombie) and flips exited so IsRunning reflects real liveness. Stop must
	// never call cmd.Wait() itself; it waits on done instead.
	go func(cmd *exec.Cmd, done chan struct{}, stopping *atomic.Bool) {
		err := cmd.Wait()
		p.exited.Store(true)
		close(done)
		switch {
		case err == nil:
			log.Debugf("[process] %s exited cleanly", name)
		case stopping.Load():
			log.Debugf("[process] %s stopped: %v", name, err)
		default:
			log.Warnf("[process] %s exited unexpectedly: %v", name, err)
		}
	}(cmd, done, stopping)
	return nil
}

// Stop stops the running process gracefully with timeout and force-kill fallback.
func (p *Runner) Stop() error {
	p.mu.Lock()
	cmd, done, stopping, already := p.cmd, p.waitDone, p.stopping, p.exited.Load()
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	// Already exited and reaped by the reaper: the PID may have been recycled,
	// so do NOT signal it. Just clear our bookkeeping.
	if already {
		p.clear(cmd)
		return nil
	}
	if stopping != nil {
		stopping.Store(true)
	}

	// Graceful SIGINT; on signal failure, force kill. We wait on the reaper's
	// done channel rather than calling cmd.Wait() (which the reaper owns).
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		<-done
		p.clear(cmd)
		return err
	}
	select {
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	case <-done:
	}
	p.clear(cmd)
	return nil
}

// clear resets the current-process bookkeeping, but only if it still refers to
// cmd — guarding against clearing a newer process installed by a concurrent
// Start/Restart.
func (p *Runner) clear(cmd *exec.Cmd) {
	p.mu.Lock()
	if p.cmd == cmd {
		p.cmd = nil
		p.waitDone = nil
		p.stopping = nil
	}
	p.mu.Unlock()
}

// Restart stops and starts the process.
func (p *Runner) Restart() error {
	if err := p.Stop(); err != nil {
		return err
	}
	return p.Start()
}

// IsRunning checks if the process is currently running.
func (p *Runner) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil && p.cmd.Process != nil && !p.exited.Load()
}

// PID returns the process ID if running, 0 otherwise.
func (p *Runner) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil && !p.exited.Load() {
		return p.cmd.Process.Pid
	}
	return 0
}

// Process returns the underlying exec.Cmd for advanced control.
// Returns nil if not running.
func (p *Runner) Process() *exec.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd
}

// SetConfig updates the configuration (e.g., for restart with new config).
func (p *Runner) SetConfig(cfg RunnerConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.config = cfg
}

// GetConfig returns a copy of the current configuration.
func (p *Runner) GetConfig() RunnerConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.config
}
