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
	return fmt.Sprintf("%s", strings.Join(errMsgs, "|"))
}
