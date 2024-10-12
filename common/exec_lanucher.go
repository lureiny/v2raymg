package common

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/lureiny/v2raymg/common/log/logger"
)

// ExecLauncher start third process and manager it
type ExecLauncher struct {
	name     string
	execPath string
	cmd      *exec.Cmd
	stdoutCh chan string
	stderrCh chan string
	restart  bool
}

// NewExecLauncher ...
func NewExecLauncher(name, execPath string, args ...string) *ExecLauncher {
	cmd := exec.Command(execPath, args...)

	return &ExecLauncher{
		name:     name,
		execPath: execPath,
		cmd:      cmd,
		stdoutCh: make(chan string),
		stderrCh: make(chan string),
		restart:  false,
	}
}

// Start ...
func (e *ExecLauncher) Start() error {
	// start process
	err := e.cmd.Start()
	if err != nil {
		return fmt.Errorf("start process[%s] fail > %v", e.name, err)
	}

	// monitor process and restart process if not except stop
	go e.monitorProcess()
	return nil
}

// Stop stop process
func (e *ExecLauncher) Stop() error {
	e.DisableRestart()

	err := e.cmd.Process.Kill()
	if err != nil {
		return fmt.Errorf("stop process[%s] fail > %v", e.name, err)
	}

	e.EnableRestart()
	return nil
}

func (e *ExecLauncher) monitorProcess() {
	for {
		err := e.cmd.Wait()
		if err != nil {
			logger.Error("process[%s] exit > %v", e.name, err)
		}

		if e.restart {
			logger.Info("restart process1...")
			err := e.Start()
			if err != nil {
				logger.Error("restart process[%s] fail > %v", e.name, err)
			}
		} else {
			break
		}
	}
}

// EnableRestart ...
func (e *ExecLauncher) EnableRestart() {
	e.restart = true
}

// DisableRestart ...
func (e *ExecLauncher) DisableRestart() {
	e.restart = false
}

// SetStdin ...
func (e *ExecLauncher) SetStdin(in *os.File) {
	e.cmd.Stdin = in
}

// GetStdout ...
func (e *ExecLauncher) GetStdout() io.Writer {
	return e.cmd.Stdout
}

// GetExecPath ...
func (e *ExecLauncher) GetExecPath() string {
	return e.execPath
}
