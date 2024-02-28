package manager

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/google/go-github/v48/github"
)

const execPath = "./"

type ProxyServer struct {
	configFile         string
	path               string
	cmd                *exec.Cmd
	isRunning          bool
	cancel             context.CancelFunc
	currentVersion     string
	expectVersion      string
	stdout             io.ReadCloser
	softwareName       string // xray/v2ray/hysteria
	softwareGithubInfo *SoftwareGithubInfo
}

func NewProxyServer(file, version, softwareName string) *ProxyServer {
	return &ProxyServer{
		configFile:         file,
		isRunning:          false,
		expectVersion:      version,
		softwareName:       softwareName,
		softwareGithubInfo: softwareGithubInfoMap[softwareName],
	}
}

func initExecFile(s *ProxyServer) error {
	_, err := os.Stat(s.path)
	if err == nil {
		return nil
	}
	if s.softwareGithubInfo == nil {
		return fmt.Errorf("softname[%v] is wrong", s.softwareName)
	}
	s.path = execPath + s.softwareGithubInfo.FileName
	// download v2ray/xray exec
	if err := s.UpdateByTagName(s.expectVersion); err != nil {
		return err
	}
	err = SwitchExec(s.softwareGithubInfo.FileName+tempShuffix, s.path)
	return err
}

func (s *ProxyServer) Start() error {
	if s.isRunning {
		return nil
	}
	if err := initExecFile(s); err != nil {
		return err
	}
	if s.softwareName == "hysteria" {
		ctx, cancel := context.WithCancel(context.Background())
		s.cmd = exec.CommandContext(ctx, s.path, "server", "-c", s.configFile)
		s.cancel = cancel
		if err := s.cmd.Start(); err != nil {
			return err
		}
		s.isRunning = true
		return nil
	}

	in, err := os.Open(s.configFile)
	if err != nil {
		return err
	}
	defer in.Close()
	ctx, cancel := context.WithCancel(context.Background())
	s.cmd = exec.CommandContext(ctx, s.path, "run")
	s.cancel = cancel
	s.cmd.Stdin = in

	if s.stdout, err = s.cmd.StdoutPipe(); err != nil {
		return err
	}

	if err := s.cmd.Start(); err != nil {
		return err
	}

	if err := s.UpdateCurrentVersion(s.stdout); err != nil {
		// 停止已经启动的进程
		s.cancel()
		return err
	}
	s.isRunning = true
	return nil
}

func (s *ProxyServer) Stop() {
	if s.isRunning {
		s.cancel()
		s.cmd.Wait()
		s.isRunning = false
	}
}

const latestTagName = "latest"
const tempShuffix = ".tmp"

func (s *ProxyServer) UpdateCurrentVersion(stdout io.ReadCloser) error {
	outInfo := make([]byte, 1024)
	_, err := stdout.Read(outInfo)
	if err != nil {
		return err
	}

	versionRegex := regexp.MustCompile(s.softwareGithubInfo.VersionRegex)
	result := versionRegex.FindSubmatch(outInfo)
	if len(result) == 0 {
		return fmt.Errorf("can not get current version > %s", outInfo)
	}
	s.currentVersion = string(result[1])
	return nil
}

// 参考: https://docs.github.com/cn/rest/releases/releases
func (s *ProxyServer) UpdateByTagName(tag string) error {
	if tag == "" {
		tag = latestTagName
	}
	if tag[0] != 'v' && tag != latestTagName {
		tag = "v" + tag
	}
	useUnzip := false
	if strings.HasSuffix(s.softwareGithubInfo.ReleaseFileName, "tar.gz") ||
		strings.HasSuffix(s.softwareGithubInfo.ReleaseFileName, ".zip") {
		useUnzip = true
	}
	repositoryRelease, err := getReleaseByTagName(tag, s.softwareGithubInfo.Owner, s.softwareGithubInfo.Repo)
	if err != nil {
		return err
	}
	downloadUrl, err := getDownloadUrl(repositoryRelease, s.softwareGithubInfo.ReleaseFileName)
	if err != nil {
		return err
	}

	zipReader, err := Download(downloadUrl, s.softwareGithubInfo.FileName, useUnzip)
	if err != nil {
		return err
	}
	if useUnzip {
		return Unzip(zipReader, s.softwareGithubInfo.FileName)
	}
	return nil
}

func (s *ProxyServer) Update(tag string) error {
	if err := s.UpdateByTagName(tag); err != nil {
		return err
	}
	s.Stop()
	if err := SwitchExec(s.softwareGithubInfo.FileName+tempShuffix, s.path); err != nil {
		return err
	}
	return s.Start()
}

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

func SwitchExec(src, dst string) error {
	os.Chmod(src, 0755)
	return os.Rename(src, dst)
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
