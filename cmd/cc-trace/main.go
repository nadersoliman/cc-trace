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
	dumpEnabled   bool
	rotateEnabled bool
	dumpDir       = "/tmp/cc-trace/dumps"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot determine home directory: %v\n", err)
		os.Exit(0)
	}
	logFilePath := filepath.Join(homeDir, ".claude", "state", "cc_trace.log")
	debugEnabled := strings.EqualFold(os.Getenv("CC_TRACE_DEBUG"), "true")
	dumpEnabled = strings.EqualFold(os.Getenv("CC_TRACE_DUMP"), "true")
	rotateEnabled = strings.EqualFold(os.Getenv("CC_TRACE_ROTATE"), "true")
	timingEnabled := strings.EqualFold(os.Getenv("CC_TRACE_TIMING"), "true")
	logging.Init(logFilePath, debugEnabled)
	logging.InitTiming(timingEnabled)
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

	// Phase 1: unmarshal base fields to determine event type.
	var base hook.HookBase
	if err := json.Unmarshal(data, &base); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to parse stdin: %v", err))
		os.Exit(0)
	}

	if dumpEnabled {
		dumpPayload(base.HookEventName, base.SessionID, data)
	}

	logging.Debug(fmt.Sprintf("Event: %s, Session: %s", base.HookEventName, base.SessionID))

	// Phase 2: unmarshal into typed struct and dispatch.
	switch base.HookEventName {
	case "PostToolUse":
		var input hook.PostToolUsePayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse PostToolUse: %v", err))
			os.Exit(0)
		}
		handlePostToolUse(input)
	case "PostToolUseFailure":
		var input hook.PostToolUseFailurePayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse PostToolUseFailure: %v", err))
			os.Exit(0)
		}
		handlePostToolUseFailure(input)
	case "SubagentStop":
		var input hook.SubagentStopPayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse SubagentStop: %v", err))
			os.Exit(0)
		}
		handleSubagentStop(input)
	case "Stop":
		var input hook.StopPayload
		if err := json.Unmarshal(data, &input); err != nil {
			logging.Log("ERROR", fmt.Sprintf("Failed to parse Stop: %v", err))
			os.Exit(0)
		}
		handleStop(input)
	default:
		logging.Debug(fmt.Sprintf("Unknown event: %s", base.HookEventName))
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

func handlePostToolUse(input hook.PostToolUsePayload) {
	start := time.Now()

	if input.SessionID == "" {
		logging.Debug("No session_id in PostToolUse, skipping")
		return
	}

	lockStart := time.Now()
	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping PostToolUse")
		return
	}
	defer state.ReleaseLock()
	lockDur := time.Since(lockStart)

	loadStart := time.Now()
	sf := state.LoadState()
	loadDur := time.Since(loadStart)

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

	saveStart := time.Now()
	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}
	saveDur := time.Since(saveStart)

	logging.Debug(fmt.Sprintf("Recorded tool: %s (%s)", input.ToolName, input.ToolUseID))

	sid := input.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	logging.Timing(fmt.Sprintf("total=%dms PostToolUse session=%s tool=%s lock=%dms load=%dms save=%dms",
		time.Since(start).Milliseconds(), sid, input.ToolName,
		lockDur.Milliseconds(), loadDur.Milliseconds(), saveDur.Milliseconds()))
}

func handlePostToolUseFailure(input hook.PostToolUseFailurePayload) {
	start := time.Now()

	if input.SessionID == "" {
		logging.Debug("No session_id in PostToolUseFailure, skipping")
		return
	}

	lockStart := time.Now()
	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping PostToolUseFailure")
		return
	}
	defer state.ReleaseLock()
	lockDur := time.Since(lockStart)

	loadStart := time.Now()
	sf := state.LoadState()
	loadDur := time.Since(loadStart)

	ss, ok := sf.Sessions[input.SessionID]
	if !ok {
		ss = &hook.SessionState{
			TranscriptPath: input.TranscriptPath,
			CWD:            input.CWD,
		}
		sf.Sessions[input.SessionID] = ss
	}

	ss.ToolSpans = append(ss.ToolSpans, hook.ToolSpanData{
		ToolName:  input.ToolName,
		ToolUseID: input.ToolUseID,
		ToolInput: input.ToolInput,
		Timestamp: time.Now(),
	})
	ss.Updated = time.Now()

	saveStart := time.Now()
	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}
	saveDur := time.Since(saveStart)

	logging.Debug(fmt.Sprintf("Recorded tool failure: %s (%s)", input.ToolName, input.ToolUseID))

	sid := input.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	logging.Timing(fmt.Sprintf("total=%dms PostToolUseFailure session=%s tool=%s lock=%dms load=%dms save=%dms",
		time.Since(start).Milliseconds(), sid, input.ToolName,
		lockDur.Milliseconds(), loadDur.Milliseconds(), saveDur.Milliseconds()))
}

func handleStop(input hook.StopPayload) {
	start := time.Now()

	// Find transcript path from input or state.
	transcriptPath := input.TranscriptPath
	sessionID := input.SessionID

	lockStart := time.Now()
	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping Stop")
		return
	}
	defer state.ReleaseLock()
	lockDur := time.Since(lockStart)

	loadStart := time.Now()
	sf := state.LoadState()
	loadDur := time.Since(loadStart)

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
	parseStart := time.Now()
	turns, totalLines, err := transcript.ParseTranscript(transcriptPath, ss.LastLine)
	parseDur := time.Since(parseStart)

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
	initStart := time.Now()
	shutdown, err := tracer.InitTracer()
	initDur := time.Since(initStart)

	if err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to init tracer: %v", err))
		return
	}
	defer shutdown() // Safety net for panics.

	exportStart := time.Now()
	tracer.ExportSessionTrace(sessionID, turns, ss.ToolSpans, ss.PendingSubagents, ss, rotateEnabled)
	exportDur := time.Since(exportStart)

	shutdownStart := time.Now()
	shutdown()
	shutdownDur := time.Since(shutdownStart)

	// Update state.
	ss.LastLine = totalLines
	ss.TurnCount += len(turns)
	ss.ToolSpans = nil
	ss.PendingSubagents = nil
	// Rotation only applies in standalone mode (no TRACEPARENT).
	// When an external trace provides the trace ID, epoch rotation is meaningless.
	if rotateEnabled && os.Getenv("TRACEPARENT") == "" {
		ss.Epoch++
		ss.SessionSpanID = ""
	}
	ss.Updated = time.Now()

	saveStart := time.Now()
	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}
	saveDur := time.Since(saveStart)

	sid := sessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	logging.Log("INFO", fmt.Sprintf("Exported %d turns in %.1fs", len(turns), time.Since(start).Seconds()))
	logging.Timing(fmt.Sprintf("total=%dms Stop session=%s lock=%dms load=%dms parse=%dms tracer_init=%dms export=%dms shutdown=%dms save=%dms turns=%d",
		time.Since(start).Milliseconds(), sid,
		lockDur.Milliseconds(), loadDur.Milliseconds(), parseDur.Milliseconds(),
		initDur.Milliseconds(), exportDur.Milliseconds(), shutdownDur.Milliseconds(),
		saveDur.Milliseconds(), len(turns)))
}

func handleSubagentStop(input hook.SubagentStopPayload) {
	start := time.Now()

	if input.SessionID == "" || input.AgentTranscriptPath == "" {
		logging.Debug("No session_id or agent_transcript_path in SubagentStop, skipping")
		return
	}

	parseStart := time.Now()
	var turns []hook.Turn
	var err error
	retries := 0
	for attempt := 0; attempt < 3; attempt++ {
		turns, _, err = transcript.ParseTranscript(input.AgentTranscriptPath, 0)
		if err == nil {
			break
		}
		retries = attempt + 1
		logging.Debug(fmt.Sprintf("Transcript not ready (attempt %d): %v", attempt+1, err))
		time.Sleep(200 * time.Millisecond)
	}
	parseDur := time.Since(parseStart)

	if err != nil {
		logging.Debug(fmt.Sprintf("Subagent transcript unavailable, skipping: %v", err))
		return
	}
	if len(turns) == 0 {
		logging.Debug("No turns in subagent transcript, skipping")
		return
	}

	lockStart := time.Now()
	if !state.AcquireLock() {
		logging.Debug("Lock held, skipping SubagentStop")
		return
	}
	defer state.ReleaseLock()
	lockDur := time.Since(lockStart)

	loadStart := time.Now()
	sf := state.LoadState()
	loadDur := time.Since(loadStart)

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

	saveStart := time.Now()
	if err := state.SaveState(sf); err != nil {
		logging.Log("ERROR", fmt.Sprintf("Failed to save state: %v", err))
	}
	saveDur := time.Since(saveStart)

	logging.Debug(fmt.Sprintf("Stored subagent %s (%s) with %d turns", input.AgentType, input.AgentID, len(turns)))

	sid := input.SessionID
	if len(sid) > 8 {
		sid = sid[:8]
	}
	logging.Timing(fmt.Sprintf("total=%dms SubagentStop session=%s agent=%s parse=%dms retries=%d lock=%dms load=%dms save=%dms",
		time.Since(start).Milliseconds(), sid, input.AgentType,
		parseDur.Milliseconds(), retries, lockDur.Milliseconds(), loadDur.Milliseconds(), saveDur.Milliseconds()))
}
