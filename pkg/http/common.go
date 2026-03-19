package http

import (
	"fmt"
	"strings"
)

func joinFailedList(failedList map[string]string) string {
	errMsgs := []string{}
	for k, v := range failedList {
		errMsgs = append(errMsgs, fmt.Sprintf("node: %s > err: %s", k, v))
	}
	return strings.Join(errMsgs, "|")
}

// splitAndFilter splits s by "," and removes empty strings.
func splitAndFilter(s string) []string {
	parts := strings.Split(s, ",")
	result := parts[:0]
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
