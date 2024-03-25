package expand

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/lureiny/v2raymg/client/http"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/global/config"
)

func reqSubFromUrl(url string) ([]string, error) {
	resp, err := http.DoGetRequest(url, nil)
	if err != nil {
		err := fmt.Errorf("get third[%s] sub fail > err: %v", url, err)
		logger.Error(err.Error())
		return nil, err
	}

	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		err := fmt.Errorf("read third[%s] sub resp fail > err: %v", url, err)
		logger.Error(err.Error())
		return nil, err
	}
	rawData, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return []string{string(data)}, nil
	}
	return strings.Split(string(rawData), "\n"), nil
}

// GetSubFromThirdUrl ...
func GetSubFromThirdUrl() ([]string, error) {
	urls := config.GetStringSlice(common.ConfigRemoteSubAddress)
	subs := []string{}
	var err error = nil
	for _, url := range urls {
		if ss, reqErr := reqSubFromUrl(url); reqErr == nil {
			subs = append(subs, ss...)
		} else {
			err = common.MergeError(err, reqErr)
		}
	}
	return subs, err
}
