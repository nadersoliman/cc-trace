package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cc-trace/internal/hook"
	"cc-trace/internal/logging"
	"cc-trace/internal/state"
	"cc-trace/internal/tracer"
	"cc-trace/internal/transcript"
)

var (
	dumpEnabled bool
	dumpDir     = "/tmp/cc-trace/dumps"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine home directory: %v\n", err)
		os.Exit(0)
	}
	logFilePath := filepath.Join(homeDir, ".claude", "state", "otel_trace_hook.log")
	debugEnabled := strings.EqualFold(os.Getenv("CC_OTEL_TRACE_DEBUG"), "true")
	dumpEnabled = strings.EqualFold(os.Getenv("CC_OTEL_TRACE_DUMP"), "true")
	logging.Init(logFilePath, debugEnabled)
	state.Init(homeDir)

	defer func() {
		if r := recover(); r != nil {
			logging.Log("ERROR", fmt.Sprintf("Panic: %v", r))
		}
	}()

	logging.Debug("Hook started")

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to read stdin: %v", err))
		os.Exit(0)
	}

	var input hook.HookInput
	if err := json.Unmarshal(data, &input); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to parse stdin: %v", err))
		os.Exit(0)
	}

	if dumpEnabled {
		dumpPayload(input.HookEventName, input.SessionID, data)
	}

	logging.Debug(fmt.Sprintf("Event: %s, Session: %s", input.HookEventName, input.SessionID))

	switch input.HookEventName {
	case "PostToolUse", "PostToolUseFailure":
		handlePostToolUse(input)
	case "SubagentStop":
		handleSubagentStop(input)
	case "Stop":
		handleStop(input)
	default:
		logging.Debug(fmt.Sprintf("Unknown event: %s", input.HookEventName))
	}
}

func dumpPayload(event, sessionID string, raw []byte) {
	_ = os.MkdirAll(dumpDir, 0o755)
	sid := sessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	ts := time.Now().Format("20060102-150405.000")
	filename := fmt.Sprintf("%s_%s_%s.json", event, sid, ts)
	path := filepath.Join(dumpDir, filename)

	// Pretty-print the JSON for readability.
	var pretty json.RawMessage
	if json.Unmarshal(raw, &pretty) == nil {
		if formatted, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			raw = formatted
		}
	}

	if err := os.WriteFile(path, raw, 0o644); err != nil {
		logging.Debug(fmt.Sprintf("Failed to dump payload: %v", err))
	} else {
		logging.Debug(fmt.Sprintf("Dumped %s payload to %s", event, path))
	}
}

func dumpTranscript(transcriptPath, sessionID string) {
	if transcriptPath == "" {
		return
	}
	_ = os.MkdirAll(dumpDir, 0o755)
	sid := sessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	ts := time.Now().Format("20060102-150405.000")
	filename := fmt.Sprintf("transcript_%s_%s.jsonl", sid, ts)
	destPath := filepath.Join(dumpDir, filename)

	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		logging.Debug(fmt.Sprintf("Failed to read transcript for dump: %v", err))
		return
	}
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		logging.Debug(fmt.Sprintf("Failed to dump transcript: %v", err))
	} else {
		logging.Debug(fmt.Sprintf("Dumped transcript to %s (%d bytes)", destPath, len(data)))
	}
}

func handlePostToolUse(input hook.HookInput) {
	if input.SessionID == "" {
		logging.Debug("No session_id in PostToolUse, skipping")
		return
	}

	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping PostToolUse")
		return
	}
	defer state.ReleaseLock()

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		ss = &hook.SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
		sf.Sessions[input.SessionID] = ss
	}

	ss.ToolSpans = append(ss.ToolSpans, hook.ToolSpanData{
		ToolName:     input.ToolName,
		ToolUseID:    input.ToolUseID,
		ToolInput:    input.ToolInput,
		ToolResponse: input.ToolResponse,
		Timestamp:    time.Now(),
	})
	ss.Updated = time.Now()

	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}

	logging.Debug(fmt.Sprintf("Recorded tool: %s (%s)", input.ToolName, input.ToolUseID))
}

func handleStop(input hook.HookInput) {
	start := time.Now()

	// Find transcript path from input or state.
	transcriptPath := input.TranscriptPath
	sessionID := input.SessionID

	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping Stop")
		return
	}
	defer state.ReleaseLock()

	sf := state.LoadState()

	// Fall back to state for session info.
	ss, ok := sf.Sessions[sessionID]
	if !ok {
		ss = &hook.SessionState{}
		sf.Sessions[sessionID] = ss
	}
	if transcriptPath == "" {
		transcriptPath = ss.TranscriptPath
	}
	if transcriptPath == "" {
		logging.Log("ERROR", "No transcript path available")
		return
	}

	if dumpEnabled {
		dumpTranscript(transcriptPath, sessionID)
	}

	// Parse transcript.
	turns, totalLines, err := transcript.ParseTranscript(transcriptPath, ss.LastLine)
	if err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to parse transcript: %v", err))
		return
	}

	if len(turns) == 0 {
		logging.Debug("No new turns to process")
		ss.LastLine = totalLines
		ss.Updated = time.Now()
		_ = state.SaveState(sf)
		return
	}

	// Renumber turns based on previous count.
	for i := range turns {
		turns[i].Number = ss.TurnCount + i + 1
	}

	// Init OTel and export.
	shutdown, err := tracer.InitTracer()
	if err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to init tracer: %v", err))
		return
	}
	defer shutdown()

	tracer.ExportSessionTrace(sessionID, turns, ss.ToolSpans, ss.PendingSubagents, ss)

	// Update state.
	ss.LastLine = totalLines
	ss.TurnCount += len(turns)
	ss.ToolSpans = nil        // Clear after export.
	ss.PendingSubagents = nil // Clear after export.
	ss.Updated = time.Now()

	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}

	duration := time.Since(start).Seconds()
	logging.Log("INFO", fmt.Sprintf("Exported %d turns in %.1fs", len(turns), duration))
}

func handleSubagentStop(input hook.HookInput) {
	if input.SessionID == "" || input.AgentTranscriptPath == "" {
		logging.Debug("No session_id or agent_transcript_path in SubagentStop, skipping")
		return
	}

	// Retry transcript read -- file may not be flushed yet when SubagentStop fires.
	var turns []hook.Turn
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		turns, _, err = transcript.ParseTranscript(input.AgentTranscriptPath, 0)
		if err == nil {
			break
		}
		logging.Debug(fmt.Sprintf("Transcript not ready (attempt %d): %v", attempt+1, err))
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		logging.Debug(fmt.Sprintf("Subagent transcript unavailable, skipping: %v", err))
		return
	}
	if len(turns) == 0 {
		logging.Debug("No turns in subagent transcript, skipping")
		return
	}

	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping SubagentStop")
		return
	}
	defer state.ReleaseLock()

	sf := state.LoadState()
	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		ss = &hook.SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
		sf.Sessions[input.SessionID] = ss
	}

	ss.PendingSubagents = append(ss.PendingSubagents, hook.PendingSubagent{
		AgentID:   input.AgentID,
		AgentType: input.AgentType,
		Turns:     turns,
	})
	ss.Updated = time.Now()

	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}

	logging.Debug(fmt.Sprintf("Stored subagent %s (%s) with %d turns", input.AgentType, input.AgentID, len(turns)))
}
