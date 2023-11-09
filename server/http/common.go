package http

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/lureiny/v2raymg/global/logger"
)

func joinFailedList(failedList map[string]string) string {
	errMsgs := []string{}
	for k, v := range failedList {
		errMsgs = append(errMsgs, fmt.Sprintf("node: %s > err: %s", k, v))
	}
	return fmt.Sprintf("%s", strings.Join(errMsgs, "|"))
}

func getIpLocation(ip string) string {
	url := "http://ip-api.com/json/" + ip
	resp, err := http.Get(url)
	if err != nil {
		logger.Error("request url[%v] fail, err: %v", url, err)
		return ""
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Error("read request url[%v] resp fail, err: %v", url, err)
		return ""
	}

	// 解析 JSON
	var data map[string]interface{}
	err = json.Unmarshal(body, &data)
	if err != nil {
		logger.Error("Unmarshal request url[%v] resp fail, err: %v", url, err)
		return ""
	}
	if regionName, ok := data["regionName"]; ok {
		return regionName.(string)
	}
	return ""
}
