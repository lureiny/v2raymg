package converter

import (
	"strings"
)

type Qv2rayConverter struct{}

func NewQv2rayConverter() Converter {
	return &Qv2rayConverter{}
}

func (c *Qv2rayConverter) Name() string {
	return qv2rayClientKeyWrod
}

func (c *Qv2rayConverter) Convert(standardUris []string) (string, error) {
	return strings.Join(standardUris, "\n"), nil
}

func init() {
	registerConverter(NewQv2rayConverter())
}
