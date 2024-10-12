package util

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-github/v48/github"
)

type SoftwareGithubInfo struct {
	Repo            string
	Owner           string
	ReleaseFileName string
	FileName        string
	VersionRegex    string
}

var softwareGithubInfoMap = map[string]*SoftwareGithubInfo{
	"xray": {
		Repo:            "Xray-core",
		Owner:           "XTLS",
		ReleaseFileName: "Xray-linux-64.zip",
		FileName:        "xray",
		VersionRegex:    `^Xray (\d+\.\d+\.\d+)`,
	},
	"hysteria": {
		Repo:            "hysteria",
		Owner:           "apernet",
		ReleaseFileName: "hysteria-linux-amd64",
		FileName:        "hysteria-linux-amd64",
		VersionRegex:    ``,
	},
}

// GetSoftwareGithubInfo ...
func GetSoftwareGithubInfo(softwareName string) *SoftwareGithubInfo {
	return softwareGithubInfoMap[softwareName]
}

const latestTagName = "latest"
const tempShuffix = ".tmp"

// Download ...
func Download(url, fileName string, useUnzip bool) (*zip.Reader, error) {
	data, err := requestUrl(url)
	if err != nil {
		return nil, err
	}
	if !useUnzip {
		// 不需要解压
		writer, err := os.Create(fileName + tempShuffix)
		if err != nil {
			return nil, err
		}
		defer writer.Close()
		writer.Write(data)
		return nil, nil
	}
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}

// DownloadByTagName 参考: https://docs.github.com/cn/rest/releases/releases
func DownloadByTagName(tag string, sgi *SoftwareGithubInfo) (string, error) {
	if sgi == nil {
		return "", fmt.Errorf("software github info is nil")
	}
	if tag == "" {
		tag = latestTagName
	}
	if tag[0] != 'v' && tag != latestTagName {
		tag = "v" + tag
	}
	useUnzip := false
	if strings.HasSuffix(sgi.ReleaseFileName, "tar.gz") ||
		strings.HasSuffix(sgi.ReleaseFileName, ".zip") {
		useUnzip = true
	}
	repositoryRelease, err := getReleaseByTagName(tag, sgi.Owner, sgi.Repo)
	if err != nil {
		return "", err
	}
	downloadUrl, err := getDownloadUrl(repositoryRelease, sgi.ReleaseFileName)
	if err != nil {
		return "", err
	}

	zipReader, err := Download(downloadUrl, sgi.FileName, useUnzip)
	if err != nil {
		return "", err
	}
	if useUnzip {
		return sgi.FileName + tempShuffix, Unzip(zipReader, sgi.FileName)
	}
	return sgi.FileName + tempShuffix, nil
}
func getReleaseByTagName(tag, owner, repo string) (*github.RepositoryRelease, error) {
	client := github.NewClient(nil)
	repositoriesService := client.Repositories
	var release *github.RepositoryRelease = nil
	var err error = nil
	if tag != latestTagName {
		release, _, err = repositoriesService.GetReleaseByTag(context.Background(), owner, repo, tag)
	} else {
		release, _, err = repositoriesService.GetLatestRelease(context.Background(), owner, repo)
	}
	return release, err
}

func getDownloadUrl(release *github.RepositoryRelease, releaseFileName string) (string, error) {
	for _, asset := range release.Assets {
		if *asset.Name == releaseFileName {
			return asset.GetBrowserDownloadURL(), nil
		}
	}
	return "", fmt.Errorf("not found release file: %s", releaseFileName)
}

func requestUrl(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	return data, err
}

// Unzip ...
func Unzip(zipReader *zip.Reader, fileName string) error {
	for _, file := range zipReader.File {
		if file.Name == fileName {
			reader, err := file.Open()
			if err != nil {
				return err
			}
			writer, err := os.Create(fileName + tempShuffix)
			if err != nil {
				return err
			}
			defer writer.Close()
			_, err = io.Copy(writer, reader)
			return err
		}
	}
	return fmt.Errorf("not found file: %s in zip file", fileName)
}
