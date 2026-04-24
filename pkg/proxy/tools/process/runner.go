// Package process provides generic process lifecycle management.
// This is a reusable infrastructure component for managing external processes.
package process

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
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

	if p.cmd != nil && p.cmd.Process != nil {
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
	return nil
}

// Stop stops the running process gracefully with timeout and force-kill fallback.
func (p *Runner) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	// Try graceful shutdown with SIGINT
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		// If signal fails, try to kill directly
		_ = p.cmd.Process.Kill()
		p.cmd.Wait()
		p.cmd = nil
		return err
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		// Timeout: force kill
		_ = p.cmd.Process.Kill()
		<-done // Wait for the goroutine to finish
	case <-done:
		// Process exited gracefully
	}

	p.cmd = nil
	return nil
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
	return p.cmd != nil && p.cmd.Process != nil
}

// PID returns the process ID if running, 0 otherwise.
func (p *Runner) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
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
