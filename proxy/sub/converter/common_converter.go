package converter

import (
	"encoding/base64"
	"strings"
)

type CommonConverter struct{}

func NewCommonConverter() Converter {
	return &CommonConverter{}
}

func (c *CommonConverter) Name() string {
	return commonClientKeyWord
}

func (c *CommonConverter) Convert(standardUris []string) (string, error) {
	// base64 encode
	uri := strings.Join(standardUris, "\n")
	return base64.StdEncoding.EncodeToString([]byte(uri)), nil
}

func init() {
	registerConverter(NewCommonConverter())
}
