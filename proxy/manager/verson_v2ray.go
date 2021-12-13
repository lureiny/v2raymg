//go:build v2ray

package manager

// 本文件记录版本管理需要的差异化内容
const (
	Repo            = "v2ray-core"
	Owner           = "v2fly"
	ReleaseFileName = "v2ray-linux-64.zip"
	FileName        = "v2ray"
	VersionRegex    = `^V2Ray (\d+\.\d+\.\d+)`
)
