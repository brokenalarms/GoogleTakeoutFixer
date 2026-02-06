package fixer

import "fmt"

type LogLevel string

const (
	LoggerInfo  LogLevel = "INFO"
	LoggerWarn  LogLevel = "WARN"
	LoggerError LogLevel = "ERROR"
)

// LogHandler allows the GUI or CLI to intercept logs
var LogHandler func(level LogLevel, message string)

// Send a log message to the handler
func Log(level LogLevel, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if LogHandler != nil {
		LogHandler(level, msg)
	}
}
