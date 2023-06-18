package logger

import (
	"github.com/lureiny/v2raymg/common/log"
)

var logger = log.Logger{}

func SetServerName(name string) {
	logger.SetServerName(name)
}

func SetNodeType(nodeType string) {
	logger.SetNodeType(nodeType)
}

func SetLogLevel(logLevel int) {
	logger.SetLogLevel(logLevel)
}

func Debug(format string, a ...interface{}) {
	logger.Debug(format, a...)
}

func Info(format string, a ...interface{}) {
	logger.Info(format, a...)
}

func Error(format string, a ...interface{}) {
	logger.Error(format, a...)
}

func Warn(format string, a ...interface{}) {
	logger.Warn(format, a...)
}

func Fatalf(format string, a ...interface{}) {
	logger.Fatalf(format, a...)
}

func init() {
	logger.Init()
}
