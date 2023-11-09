package converter

import (
	"strings"
)

var globalConverters = map[string]Converter{}

func registerConverter(c Converter) {
	globalConverters[c.Name()] = c
}

func ConvertSubUri(client string, standardUris []string, opts ...Opt) (string, error) {
	for k, c := range globalConverters {
		if strings.Contains(client, k) {
			return c.Convert(standardUris, opts...)
		}
	}
	return globalConverters[commonClientKeyWord].Convert(standardUris)
}
