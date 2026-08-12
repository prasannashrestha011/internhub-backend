package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

type Logger struct {
	*log.Logger
	level Level
}

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

var (
	logLevelNames = map[Level]string{
		DEBUG: "DEBUG",
		INFO:  "INFO",
		WARN:  "WARN",
		ERROR: "ERROR",
		FATAL: "FATAL",
	}
)

func New(level string) *Logger {
	logLevel := INFO
	switch level {
	case "debug":
		logLevel = DEBUG
	case "info":
		logLevel = INFO
	case "warn":
		logLevel = WARN
	case "error":
		logLevel = ERROR
	case "fatal":
		logLevel = FATAL
	}

	return &Logger{
		Logger: log.New(os.Stdout, "", 0),
		level:  logLevel,
	}
}

func (l *Logger) Debug(msg string, args ...interface{}) {
	if l.level <= DEBUG {
		l.log(DEBUG, msg, args...)
	}
}

func (l *Logger) Info(msg string, args ...interface{}) {
	if l.level <= INFO {
		l.log(INFO, msg, args...)
	}
}

func (l *Logger) Warn(msg string, args ...interface{}) {
	if l.level <= WARN {
		l.log(WARN, msg, args...)
	}
}

func (l *Logger) Error(msg string, args ...interface{}) {
	if l.level <= ERROR {
		l.log(ERROR, msg, args...)
	}
}

func (l *Logger) Fatal(msg string, args ...interface{}) {
	if l.level <= FATAL {
		l.log(FATAL, msg, args...)
		os.Exit(1)
	}
}

func (l *Logger) log(level Level, msg string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	levelStr := logLevelNames[level]

	var output string
	if len(args) > 0 {
		output = fmt.Sprintf("[%s] %s: %s | %v", timestamp, levelStr, msg, args)
	} else {
		output = fmt.Sprintf("[%s] %s: %s", timestamp, levelStr, msg)
	}

	l.Logger.Println(output)
}
