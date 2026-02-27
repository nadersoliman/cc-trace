package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	homeDir      string
	logFilePath  string
	debugEnabled bool
)

func init() {
	var err error
	homeDir, err = os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine home directory: %v\n", err)
		os.Exit(0)
	}
	logFilePath = filepath.Join(homeDir, ".claude", "state", "otel_trace_hook.log")
	debugEnabled = strings.EqualFold(os.Getenv("CC_OTEL_TRACE_DEBUG"), "true")
	initStatePaths(homeDir)
}

func logMsg(level, message string) {
	dir := filepath.Dir(logFilePath)
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "%s [%s] %s\n", ts, level, message)
}

func debugLog(message string) {
	if debugEnabled {
		logMsg("DEBUG", message)
	}
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			logMsg("ERROR", fmt.Sprintf("Panic: %v", r))
		}
	}()

	debugLog("Hook started")

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to read stdin: %v", err))
		os.Exit(0)
	}

	var input HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to parse stdin: %v", err))
		os.Exit(0)
	}

	debugLog(fmt.Sprintf("Event: %s, Session: %s", input.HookEventName, input.SessionID))

	switch input.HookEventName {
	case "PostToolUse", "PostToolUseFailure":
		handlePostToolUse(input)
	case "SubagentStop":
		handleSubagentStop(input)
	case "Stop":
		handleStop(input)
	default:
		debugLog(fmt.Sprintf("Unknown event: %s", input.HookEventName))
	}
}

func handlePostToolUse(input HookInput) {
	if input.SessionID == "" {
		debugLog("No session_id in PostToolUse, skipping")
		return
	}

	if !acquireLock() {
		debugLog("Lock held, skipping PostToolUse")
		return
	}
	defer releaseLock()

	sf := loadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		ss = &SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
		sf.Sessions[input.SessionID] = ss
	}

	ss.ToolSpans = append(ss.ToolSpans, ToolSpanData{
		ToolName:     input.ToolName,
		ToolUseID:    input.ToolUseID,
		ToolInput:    input.ToolInput,
		ToolResponse: input.ToolResponse,
		Timestamp:    time.Now(),
	})
	ss.Updated = time.Now()

	if err := saveState(sf); err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}

	debugLog(fmt.Sprintf("Recorded tool: %s (%s)", input.ToolName, input.ToolUseID))
}

func handleStop(input HookInput) {
	start := time.Now()

	// Find transcript path from input or state.
	transcriptPath := input.TranscriptPath
	sessionID := input.SessionID

	if !acquireLock() {
		debugLog("Lock held, skipping Stop")
		return
	}
	defer releaseLock()

	sf := loadState()

	// Fall back to state for session info.
	ss, ok := sf.Sessions[sessionID]
	if !ok {
		ss = &SessionState{}
		sf.Sessions[sessionID] = ss
	}
	if transcriptPath == "" {
		transcriptPath = ss.TranscriptPath
	}
	if transcriptPath == "" {
		logMsg("ERROR", "No transcript path available")
		return
	}

	// Parse transcript.
	turns, totalLines, err := parseTranscript(transcriptPath, ss.LastLine)
	if err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to parse transcript: %v", err))
		return
	}

	if len(turns) == 0 {
		debugLog("No new turns to process")
		ss.LastLine = totalLines
		ss.Updated = time.Now()
		_ = saveState(sf)
		return
	}

	// Renumber turns based on previous count.
	for i := range turns {
		turns[i].Number = ss.TurnCount + i + 1
	}

	// Init OTel and export.
	shutdown, err := initTracer()
	if err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to init tracer: %v", err))
		return
	}
	defer shutdown()

	exportSessionTrace(sessionID, turns, ss.ToolSpans, ss.PendingSubagents, ss)

	// Update state.
	ss.LastLine = totalLines
	ss.TurnCount += len(turns)
	ss.ToolSpans = nil          // Clear after export.
	ss.PendingSubagents = nil   // Clear after export.
	ss.Updated = time.Now()

	if err := saveState(sf); err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}

	duration := time.Since(start).Seconds()
	logMsg("INFO", fmt.Sprintf("Exported %d turns in %.1fs", len(turns), duration))
}

func handleSubagentStop(input HookInput) {
	if input.SessionID == "" || input.AgentTranscriptPath == "" {
		debugLog("No session_id or agent_transcript_path in SubagentStop, skipping")
		return
	}

	// Retry transcript read — file may not be flushed yet when SubagentStop fires.
	var turns []Turn
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		turns, _, err = parseTranscript(input.AgentTranscriptPath, 0)
		if err == nil {
			break
		}
		debugLog(fmt.Sprintf("Transcript not ready (attempt %d): %v", attempt+1, err))
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		debugLog(fmt.Sprintf("Subagent transcript unavailable, skipping: %v", err))
		return
	}
	if len(turns) == 0 {
		debugLog("No turns in subagent transcript, skipping")
		return
	}

	if !acquireLock() {
		debugLog("Lock held, skipping SubagentStop")
		return
	}
	defer releaseLock()

	sf := loadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		ss = &SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
		sf.Sessions[input.SessionID] = ss
	}

	ss.PendingSubagents = append(ss.PendingSubagents, PendingSubagent{
		AgentID:   input.AgentID,
		AgentType: input.AgentType,
		Turns:     turns,
	})
	ss.Updated = time.Now()

	if err := saveState(sf); err != nil {
		logMsg("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}

	debugLog(fmt.Sprintf("Stored subagent %s (%s) with %d turns", input.AgentType, input.AgentID, len(turns)))
}
