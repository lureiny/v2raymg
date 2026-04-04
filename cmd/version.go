package cmd

import "github.com/lureiny/v2raymg/pkg/buildinfo"

// Version, Commit, BuildTime are now stored in pkg/buildinfo and injected
// via -ldflags at build time. These accessors exist for backward compatibility.
func GetVersion() string   { return buildinfo.Version }
func GetCommit() string    { return buildinfo.Commit }
func GetBuildTime() string { return buildinfo.BuildTime }
