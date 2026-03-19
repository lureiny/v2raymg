// Package container provides generic container process management.
// This package contains reusable components for managing proxy containers.
//
// Note: The generic process runner has been moved to tools/process.
// This file provides backward compatibility aliases.
package container

import (
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
)

// ProcessRunnerConfig is an alias for process.RunnerConfig.
// Deprecated: Use tools/process.RunnerConfig instead.
type ProcessRunnerConfig = process.RunnerConfig

// ProcessRunner is an alias for process.Runner.
// Deprecated: Use tools/process.Runner instead.
type ProcessRunner = process.Runner

// NewProcessRunner creates a new ProcessRunner.
// Deprecated: Use tools/process.NewRunner instead.
var NewProcessRunner = process.NewRunner
