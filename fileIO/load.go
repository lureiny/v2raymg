package fileIO

import (
	"encoding/json"
	"io/ioutil"
)

// LoadConfig load config from file
func LoadConfig(file string) (*V2rayConfig, error) {
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var config V2rayConfig
	err = json.Unmarshal(content, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
