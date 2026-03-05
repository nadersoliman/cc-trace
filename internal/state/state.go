package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"
)

var (
	stateFilePath string
	lockFilePath  string
)

// Init configures the state file and lock file paths under homeDir.
func Init(homeDir string) {
	stateDir := filepath.Join(homeDir, ".claude", "state")
	stateFilePath = filepath.Join(stateDir, "cc_trace_state.json")
	lockFilePath = filepath.Join(stateDir, "cc_trace_state.lock")
}

const staleLockAge = 5 * time.Minute
const staleSessionAge = 24 * time.Hour

// AcquireLock attempts to create an exclusive lock file.
// Returns true if the lock was acquired, false otherwise.
// Stale locks older than staleLockAge are automatically removed.
func AcquireLock() bool {
	if info, err := os.Stat(lockFilePath); err == nil {
		if time.Since(info.ModTime()) > staleLockAge {
			logging.Debug("Removing stale lock file")
			_ = os.Remove(lockFilePath)
		}
	}
	f, err := os.OpenFile(lockFilePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		logging.Debug(fmt.Sprintf("Could not acquire lock: %v", err))
		return false
	}
	f.Close()
	return true
}

// ReleaseLock removes the lock file.
func ReleaseLock() {
	if err := os.Remove(lockFilePath); err != nil && !os.IsNotExist(err) {
		logging.Debug(fmt.Sprintf("Could not remove lock file: %v", err))
	}
}

// LoadState reads and parses the state file from disk.
// Returns an empty StateFile if the file does not exist or is corrupt.
func LoadState() *hook.StateFile {
	data, err := os.ReadFile(stateFilePath)
	if err != nil {
		return &hook.StateFile{Sessions: make(map[string]*hook.SessionState)}
	}
	var sf hook.StateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return &hook.StateFile{Sessions: make(map[string]*hook.SessionState)}
	}
	if sf.Sessions == nil {
		sf.Sessions = make(map[string]*hook.SessionState)
	}
	return &sf
}

// SaveState writes the state file to disk, pruning sessions older than
// staleSessionAge.
func SaveState(sf *hook.StateFile) error {
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
