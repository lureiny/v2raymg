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

func (c *CommonConverter) Convert(standardUris []string, opts ...Opt) (string, error) {
	for _, opt := range opts {
		standardUris = opt(standardUris)
	}
	// base64 encode
	uri := strings.Join(standardUris, "\n")
	return base64.StdEncoding.EncodeToString([]byte(uri)), nil
}

func init() {
	registerConverter(NewCommonConverter())
}
