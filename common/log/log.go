package log

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Logger struct {
	baseLogger *log.Logger
	serverName string
	nodeType   string // node type: Center / End
	logLevel   int
}

const (
	DEBUG_LEVEL = 0
	INFO_LEVEL  = 1
	WARN_LEVEL  = 2
	ERR_LEVEL   = 3
)

var logLevelPrefixMap = map[int]string{
	DEBUG_LEVEL: "DEBUG",
	INFO_LEVEL:  "INFO",
	WARN_LEVEL:  "WARN",
	ERR_LEVEL:   "ERR",
}

const baseLogString = "%s|File=%s:%d|Func=%s|NodeType=%s|ServerName=%s|%s"

func (logger *Logger) Init() {
	logger.baseLogger = log.New(os.Stdout, "", log.Ldate|log.LUTC|log.Lmicroseconds)
	logger.logLevel = INFO_LEVEL
}

func (logger *Logger) SetServerName(name string) {
	logger.serverName = name
}

func (logger *Logger) SetNodeType(nodeType string) {
	logger.nodeType = nodeType
}

func (logger *Logger) SetLogLevel(logLevel int) {
	logger.logLevel = logLevel
}

func (logger *Logger) Debug(format string, a ...interface{}) {
	logger.baseLogOut(DEBUG_LEVEL, format, a...)
}

func (logger *Logger) Info(format string, a ...interface{}) {
	logger.baseLogOut(INFO_LEVEL, format, a...)
}

func (logger *Logger) Error(format string, a ...interface{}) {
	logger.baseLogOut(ERR_LEVEL, format, a...)
}

func (logger *Logger) Warn(format string, a ...interface{}) {
	logger.baseLogOut(WARN_LEVEL, format, a...)
}

func (logger *Logger) Fatalf(format string, a ...interface{}) {
	logger.baseLogOut(ERR_LEVEL, format, a...)
	os.Exit(-1)
}

func (logger *Logger) baseLogOut(logLevel int, format string, a ...interface{}) {
	if logLevel >= logger.logLevel {
		pc, file, line, _ := runtime.Caller(2)
		file = filepath.Base(file)
		longFuncName := filepath.Base(runtime.FuncForPC(pc).Name())
		index := strings.Index(longFuncName, ".")
		funcName := longFuncName
		if index > 0 {
			funcName = longFuncName[index+1:]
		}
		format = fmt.Sprintf(
			baseLogString,
			logLevelPrefixMap[logLevel],
			file,
			line,
			funcName,
			logger.nodeType,
			logger.serverName,
			format,
		)
		logger.baseLogger.Printf(format, a...)
	}
}
