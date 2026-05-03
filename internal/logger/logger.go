package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"
)

// Logger writes structured, timestamped log lines to both stdout/stderr and a file.
type Logger struct {
	info  *log.Logger
	warn  *log.Logger
	err   *log.Logger
	debug *log.Logger
	file  *os.File
	dbg   bool
}

func New(logFile string, debug bool) (*Logger, error) {
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file %q: %w", logFile, err)
	}

	both := io.MultiWriter(os.Stdout, f)
	errBoth := io.MultiWriter(os.Stderr, f)

	return &Logger{
		info:  log.New(both, "", 0),
		warn:  log.New(both, "", 0),
		err:   log.New(errBoth, "", 0),
		debug: log.New(both, "", 0),
		file:  f,
		dbg:   debug,
	}, nil
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

func now() string {
	return time.Now().Format("2006-01-02 15:04:05.000")
}

func (l *Logger) Info(msg string) {
	l.info.Printf("[%s] INFO  %s", now(), msg)
}

func (l *Logger) Infof(format string, args ...any) {
	l.Info(fmt.Sprintf(format, args...))
}

func (l *Logger) Warn(msg string) {
	l.warn.Printf("[%s] WARN  %s", now(), msg)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.Warn(fmt.Sprintf(format, args...))
}

func (l *Logger) Error(msg string) {
	l.err.Printf("[%s] ERROR %s", now(), msg)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.Error(fmt.Sprintf(format, args...))
}

func (l *Logger) Debug(msg string) {
	if l.dbg {
		l.debug.Printf("[%s] DEBUG %s", now(), msg)
	}
}

func (l *Logger) Debugf(format string, args ...any) {
	if l.dbg {
		l.Debug(fmt.Sprintf(format, args...))
	}
}
