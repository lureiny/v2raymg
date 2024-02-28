package manager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lureiny/v2raymg/server/rpc/proto"
)

const hyProxyName = "hysteria"
const hyTrafficType = "user"

type HysteriaTraffic struct {
	TX int64 `json:"tx"`
	RX int64 `json:"rx"`
}

// QueryHysteriaStats ...
func QueryHysteriaStats(host, secret string, clear bool) (map[string]*proto.Stats, error) {
	hosts := strings.Split(host, ":")
	if len(hosts) != 2 {
		return nil, fmt.Errorf("hysteria traffic listen config err > host[%v]", host)
	}
	if hosts[0] == "" {
		hosts[0] = "127.0.0.1"
	}
	params := ""
	if clear {
		params += "&clear=1"
	}
	reqUrl := "http://" + strings.Join(hosts, ":") + "/traffic"
	if params != "" {
		reqUrl += "?" + params
	}
	req, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("get new request fail > %v", err)
	}
	if secret != "" {
		req.Header.Add("Authorization", secret)
	}
	client := http.Client{Timeout: 1 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("req hystreia traffic fail > %v", err)
	}
	defer resp.Body.Close()
	hyStats := map[string]*HysteriaTraffic{}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read hystreia traffic fail > %v", err)
	}
	if err := json.Unmarshal(data, &hyStats); err != nil {
		return nil, fmt.Errorf("unmarshal hystreia traffic fail > %v", err)
	}
	result := map[string]*proto.Stats{}
	for name, stat := range hyStats {
		result[hyProxyName+"_"+name] = &proto.Stats{
			Name:     name,
			Type:     hyTrafficType,
			Downlink: stat.TX,
			Uplink:   stat.RX,
			Proxy:    hyProxyName,
		}
	}
	return result, nil
}
