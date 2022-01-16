package fileIO

import (
	"encoding/json"
	"io/ioutil"

	"github.com/lureiny/v2raymg/protocol"
)

// DumpConfig write config to file
func DumpConfig(c *protocol.V2rayConfig, fileName string) error {
	data, err := json.MarshalIndent(c, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(fileName, data, 0777)
}
