package hook

import "time"

// HookBase contains fields shared across all hook event payloads.
type HookBase struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	CWD            string `json:"cwd"`
	PermissionMode string `json:"permission_mode"`
	HookEventName  string `json:"hook_event_name"`
}

// PostToolUsePayload is the schema for PostToolUse hook events.
type PostToolUsePayload struct {
	HookBase
	ToolName     string                 `json:"tool_name"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse interface{}            `json:"tool_response,omitempty"`
	ToolUseID    string                 `json:"tool_use_id"`
}

// PostToolUseFailurePayload is the schema for PostToolUseFailure hook events.
type PostToolUseFailurePayload struct {
	HookBase
	ToolName    string                 `json:"tool_name"`
	ToolInput   map[string]interface{} `json:"tool_input,omitempty"`
	ToolUseID   string                 `json:"tool_use_id"`
	Error       string                 `json:"error"`
	IsInterrupt bool                   `json:"is_interrupt"`
	AgentID     string                 `json:"agent_id,omitempty"`
	AgentType   string                 `json:"agent_type,omitempty"`
}

// SubagentStopPayload is the schema for SubagentStop hook events.
type SubagentStopPayload struct {
	HookBase
	AgentID             string `json:"agent_id"`
	AgentType           string `json:"agent_type"`
	AgentTranscriptPath string `json:"agent_transcript_path"`
	LastAssistantMsg    string `json:"last_assistant_message"`
	StopHookActive      bool   `json:"stop_hook_active"`
}

// StopPayload is the schema for Stop hook events.
type StopPayload struct {
	HookBase
	StopHookActive   bool   `json:"stop_hook_active"`
	LastAssistantMsg string `json:"last_assistant_message"`
}

// ToolSpanData is recorded by PostToolUse for later use by Stop.
type ToolSpanData struct {
	ToolName     string                 `json:"tool_name"`
	ToolUseID    string                 `json:"tool_use_id"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	ToolResponse interface{}            `json:"tool_response,omitempty"`
	Timestamp    time.Time              `json:"timestamp"`
}

// PendingSubagent holds parsed subagent data awaiting export at parent Stop time.
type PendingSubagent struct {
	AgentID   string `json:"agent_id"`
	AgentType string `json:"agent_type"`
	Turns     []Turn `json:"turns"`
}

// SessionState persists between hook invocations for one session.
type SessionState struct {
	SessionSpanID    string            `json:"session_span_id"`
	SessionStart     time.Time         `json:"session_start"`
	TranscriptPath   string            `json:"transcript_path"`
	CWD              string            `json:"cwd"`
	LastLine         int               `json:"last_line"`
	TurnCount        int               `json:"turn_count"`
	Epoch            int               `json:"epoch"`
	ToolSpans        []ToolSpanData    `json:"tool_spans"`
	PendingSubagents []PendingSubagent `json:"pending_subagents,omitempty"`
	Updated          time.Time         `json:"updated"`
}

// StateFile is the top-level structure persisted to disk.
type StateFile struct {
	Sessions map[string]*SessionState `json:"sessions"`
}

// Turn represents a parsed conversation turn from the transcript.
type Turn struct {
	Number              int
	UserText            string
	UserTimestamp       time.Time
	AssistantMessages   []map[string]interface{}
	ToolCalls           []ToolCall
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	StopReason          string
	DurationMs          int
	StartTime           time.Time
	EndTime             time.Time
}

// ToolCall represents a tool_use block matched with its result.
type ToolCall struct {
	Name      string
	ID        string
	Input     interface{}
	Output    interface{}
	StartTime time.Time
	EndTime   time.Time
	Success   bool
}
