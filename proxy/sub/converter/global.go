package converter

import (
	"strings"
)

var globalConverters = map[string]Converter{}

func registerConverter(c Converter) {
	globalConverters[c.Name()] = c
}

func ConvertSubUri(client string, standardUris []string) (string, error) {
	for k, c := range globalConverters {
		if strings.Contains(client, k) {
			return c.Convert(standardUris)
		}
	}
	return globalConverters[commonClientKeyWord].Convert(standardUris)
}
