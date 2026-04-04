package buildinfo

// Build-time variables injected via -ldflags:
//
//	-X github.com/lureiny/v2raymg/pkg/buildinfo.Version=v1.2.3
//	-X github.com/lureiny/v2raymg/pkg/buildinfo.Commit=abc1234
//	-X github.com/lureiny/v2raymg/pkg/buildinfo.BuildTime=2026-03-23T08:00:00Z
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)
