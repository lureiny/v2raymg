package http

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

func jsonOK(c *gin.Context) {
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func jsonErr(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "msg": msg})
}

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
