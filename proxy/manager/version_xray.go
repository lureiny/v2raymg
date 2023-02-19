//go:build !v2ray

package manager

// 本文件记录版本管理需要的差异化内容
const (
	Repo            = "Xray-core"
	Owner           = "XTLS"
	ReleaseFileName = "Xray-linux-64.zip"
	FileName        = "xray"
	VersionRegex    = `^Xray (\d+\.\d+\.\d+)`
)
