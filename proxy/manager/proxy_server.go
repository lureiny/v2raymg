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

	"github.com/google/go-github/v48/github"
)

const execPath = "./" + FileName

type ProxyServer struct {
	configFile     string
	path           string
	cmd            *exec.Cmd
	isRunning      bool
	cancel         context.CancelFunc
	currentVersion string
	expectVersion  string
	stdout         io.ReadCloser
	certManager    *CertManager
}

func NewProxyServer(file, version string) *ProxyServer {
	return &ProxyServer{
		configFile:    file,
		path:          execPath,
		isRunning:     false,
		expectVersion: version,
	}
}

func initExecFile(s *ProxyServer) error {
	_, err := os.Stat(s.path)
	if err == nil {
		return nil
	}
	// download v2ray/xray exec
	if err := s.UpdateByTagName(s.expectVersion); err != nil {
		return err
	}
	err = SwitchExec(FileName+tempShuffix, s.path)
	return err
}

func (s *ProxyServer) Start() error {
	if s.isRunning {
		return nil
	}
	if err := initExecFile(s); err != nil {
		return err
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

	versionRegex := regexp.MustCompile(VersionRegex)
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
	repositoryRelease, err := getReleaseByTagName(tag)
	if err != nil {
		return err
	}
	downloadUrl, err := getDownloadUrl(repositoryRelease)
	if err != nil {
		return err
	}

	zipReader, err := Download(downloadUrl)
	if err != nil {
		return err
	}
	return Unzip(zipReader)
}

func (s *ProxyServer) Update(tag string) error {
	if err := s.UpdateByTagName(tag); err != nil {
		return err
	}
	s.Stop()
	if err := SwitchExec(FileName+tempShuffix, s.path); err != nil {
		return err
	}
	return s.Start()
}

func Download(url string) (*zip.Reader, error) {
	data, err := requestUrl(url)
	if err != nil {
		return nil, err
	}
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}

func Unzip(zipReader *zip.Reader) error {
	for _, file := range zipReader.File {
		if file.Name == FileName {
			reader, err := file.Open()
			if err != nil {
				return err
			}
			writer, err := os.Create(FileName + tempShuffix)
			if err != nil {
				return err
			}
			defer writer.Close()
			_, err = io.Copy(writer, reader)
			return err
		}
	}
	return fmt.Errorf("not found file: %s in zip file", FileName)
}

func SwitchExec(src, dst string) error {
	os.Chmod(src, 0755)
	return os.Rename(src, dst)
}

func getReleaseByTagName(tag string) (*github.RepositoryRelease, error) {
	client := github.NewClient(nil)
	repositoriesService := client.Repositories
	var release *github.RepositoryRelease = nil
	var err error = nil
	if tag != latestTagName {
		release, _, err = repositoriesService.GetReleaseByTag(context.Background(), Owner, Repo, tag)
	} else {
		release, _, err = repositoriesService.GetLatestRelease(context.Background(), Owner, Repo)
	}
	return release, err
}

func getDownloadUrl(release *github.RepositoryRelease) (string, error) {
	for _, asset := range release.Assets {
		if *asset.Name == ReleaseFileName {
			return asset.GetBrowserDownloadURL(), nil
		}
	}
	return "", fmt.Errorf("not found release file: %s", ReleaseFileName)
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
