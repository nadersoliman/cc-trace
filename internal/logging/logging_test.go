package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimingEnabled(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")
	Init(logFile, false)
	InitTiming(true)

	Timing("total=5ms PostToolUse session=abc tool=Read")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[TIMING]") {
		t.Errorf("expected [TIMING] in log, got: %s", content)
	}
	if !strings.Contains(content, "total=5ms") {
		t.Errorf("expected timing message in log, got: %s", content)
	}
}

func TestTimingDisabled(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")
	Init(logFile, false)
	InitTiming(false)

	Timing("total=5ms PostToolUse session=abc tool=Read")

	_, err := os.ReadFile(logFile)
	if err == nil {
		t.Error("expected no log file when timing is disabled")
	}
}
