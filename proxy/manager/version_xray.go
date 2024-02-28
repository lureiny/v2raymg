//go:build !v2ray

package manager

type SoftwareGithubInfo struct {
	Repo            string
	Owner           string
	ReleaseFileName string
	FileName        string
	VersionRegex    string
}

var softwareGithubInfoMap = map[string]*SoftwareGithubInfo{
	"xray": &SoftwareGithubInfo{
		Repo:            "Xray-core",
		Owner:           "XTLS",
		ReleaseFileName: "Xray-linux-64.zip",
		FileName:        "xray",
		VersionRegex:    `^Xray (\d+\.\d+\.\d+)`,
	},
	"hysteria": &SoftwareGithubInfo{
		Repo:            "hysteria",
		Owner:           "apernet",
		ReleaseFileName: "hysteria-linux-amd64",
		FileName:        "hysteria-linux-amd64",
		VersionRegex:    ``,
	},
}
