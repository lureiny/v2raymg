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

type SoftwareGithubInfo struct {
	Repo            string
	Owner           string
	ReleaseFileName string
	FileName        string
	VersionRegex    string
}

var softwareGithubInfoMap = map[string]*SoftwareGithubInfo{
	"v2ray": &SoftwareGithubInfo{
		Repo:            "v2ray-core",
		Owner:           "v2fly",
		ReleaseFileName: "v2ray-linux-64.zip",
		FileName:        "v2ray",
		VersionRegex:    `^V2Ray (\d+\.\d+\.\d+)`,
	},
	"hysteria": &SoftwareGithubInfo{
		Repo:            "hysteria",
		Owner:           "apernet",
		ReleaseFileName: "hysteria-linux-amd64",
		FileName:        "hysteria-linux-amd64",
		VersionRegex:    ``,
	},
}
