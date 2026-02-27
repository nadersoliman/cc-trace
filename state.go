package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	stateFilePath string
	lockFilePath  string
)

func initStatePaths(homeDir string) {
	stateDir := filepath.Join(homeDir, ".claude", "state")
	stateFilePath = filepath.Join(stateDir, "otel_trace_state.json")
	lockFilePath = filepath.Join(stateDir, "otel_trace_state.lock")
}

const staleLockAge = 5 * time.Minute
const staleSessionAge = 24 * time.Hour

func acquireLock() bool {
	if info, err := os.Stat(lockFilePath); err == nil {
		if time.Since(info.ModTime()) > staleLockAge {
			debugLog("Removing stale lock file")
			_ = os.Remove(lockFilePath)
		}
	}
	f, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		debugLog(fmt.Sprintf("Could not acquire lock: %v", err))
		return false
	}
	f.Close()
	return true
}

func releaseLock() {
	if err := os.Remove(lockFilePath); err != nil && !os.IsNotExist(err) {
		debugLog(fmt.Sprintf("Could not remove lock file: %v", err))
	}
}

func loadState() *StateFile {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return &StateFile{Sessions: make(map[string]*SessionState)}
	}
	var sf StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return &StateFile{Sessions: make(map[string]*SessionState)}
	}
	if sf.Sessions == nil {
		sf.Sessions = make(map[string]*SessionState)
	}
	return &sf
}

func saveState(sf *StateFile) error {
	// Prune stale sessions.
	for id, ss := range sf.Sessions {
		if time.Since(ss.Updated) > staleSessionAge {
			delete(sf.Sessions, id)
		}
	}

	dir := filepath.Dir(stateFilePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(stateFilePath, data, 0o644)
}
