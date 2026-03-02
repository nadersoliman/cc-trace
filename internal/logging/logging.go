package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	filePath     string
	debugEnabled bool
)

// Init configures the logging subsystem.
func Init(logFilePath string, debug bool) {
	filePath = logFilePath
	debugEnabled = debug
}

// Log writes a timestamped message at the given level to the log file.
func Log(level, message string) {
	dir := filepath.Dir(filePath)
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "%s [%s] %s\n", ts, level, message)
}

// Debug logs a message at DEBUG level if debug mode is enabled.
func Debug(message string) {
	if debugEnabled {
		Log("DEBUG", message)
	}
}
