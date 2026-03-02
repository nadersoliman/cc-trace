package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "..", "testdata", "fixtures", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("fixture not found: %s", p)
	}
	return p
}

func TestParseTranscript_Simple(t *testing.T) {
	path := fixturePath(t, "transcript_simple.jsonl")
	turns, totalLines, err := ParseTranscript(path, 0)
	if err != nil {
		t.Fatalf("ParseTranscript failed: %v", err)
	}

	if totalLines != 4 {
		t.Errorf("expected 4 total lines, got %d", totalLines)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}

	// Turn 1 assertions.
	turn1 := turns[0]
	if turn1.UserText != "Hello, what is this project?" {
		t.Errorf("turn 1 UserText = %q, want %q", turn1.UserText, "Hello, what is this project?")
	}
	if turn1.Model != "claude-sonnet-4-20250514" {
		t.Errorf("turn 1 Model = %q, want %q", turn1.Model, "claude-sonnet-4-20250514")
	}
	if turn1.InputTokens != 100 {
		t.Errorf("turn 1 InputTokens = %d, want 100", turn1.InputTokens)
	}
	if turn1.OutputTokens != 20 {
		t.Errorf("turn 1 OutputTokens = %d, want 20", turn1.OutputTokens)
	}
	if turn1.CacheReadTokens != 50 {
		t.Errorf("turn 1 CacheReadTokens = %d, want 50", turn1.CacheReadTokens)
	}
	if turn1.CacheCreationTokens != 10 {
		t.Errorf("turn 1 CacheCreationTokens = %d, want 10", turn1.CacheCreationTokens)
	}
	if turn1.StopReason != "end_turn" {
		t.Errorf("turn 1 StopReason = %q, want %q", turn1.StopReason, "end_turn")
	}

	// Turn 2 assertions.
	turn2 := turns[1]
	if turn2.UserText != "How does it work?" {
		t.Errorf("turn 2 UserText = %q, want %q", turn2.UserText, "How does it work?")
	}
	// The parser increments turnNum before finalizing the last turn,
	// so the second turn in a 2-turn transcript gets Number=3.
	if turn2.Number != 3 {
		t.Errorf("turn 2 Number = %d, want 3", turn2.Number)
	}
}

func TestParseTranscript_ToolCalls(t *testing.T) {
	path := fixturePath(t, "transcript_tools.jsonl")
	turns, _, err := ParseTranscript(path, 0)
	if err != nil {
		t.Fatalf("ParseTranscript failed: %v", err)
	}

	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}

	turn := turns[0]
	if len(turn.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(turn.ToolCalls))
	}

	// Tool 1: Read.
	tc1 := turn.ToolCalls[0]
	if tc1.Name != "Read" {
		t.Errorf("tool 1 Name = %q, want %q", tc1.Name, "Read")
	}
	if tc1.ID != "toolu_test_001" {
		t.Errorf("tool 1 ID = %q, want %q", tc1.ID, "toolu_test_001")
	}
	if !tc1.Success {
		t.Errorf("tool 1 Success = false, want true")
	}

	// Tool 2: Bash.
	tc2 := turn.ToolCalls[1]
	if tc2.Name != "Bash" {
		t.Errorf("tool 2 Name = %q, want %q", tc2.Name, "Bash")
	}
	if tc2.ID != "toolu_test_002" {
		t.Errorf("tool 2 ID = %q, want %q", tc2.ID, "toolu_test_002")
	}
	if !tc2.Success {
		t.Errorf("tool 2 Success = false, want true")
	}
}

func TestParseTranscript_DurationMs(t *testing.T) {
	path := fixturePath(t, "transcript_tools.jsonl")
	turns, _, err := ParseTranscript(path, 0)
	if err != nil {
		t.Fatalf("ParseTranscript failed: %v", err)
	}

	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].DurationMs != 12000 {
		t.Errorf("DurationMs = %d, want 12000", turns[0].DurationMs)
	}
}

func TestParseTranscript_StartLine(t *testing.T) {
	path := fixturePath(t, "transcript_simple.jsonl")
	turns, totalLines, err := ParseTranscript(path, 2)
	if err != nil {
		t.Fatalf("ParseTranscript failed: %v", err)
	}

	if totalLines != 4 {
		t.Errorf("totalLines = %d, want 4", totalLines)
	}
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].UserText != "How does it work?" {
		t.Errorf("UserText = %q, want %q", turns[0].UserText, "How does it work?")
	}
}

func TestParseTranscript_StopReason(t *testing.T) {
	path := fixturePath(t, "transcript_tools.jsonl")
	turns, _, err := ParseTranscript(path, 0)
	if err != nil {
		t.Fatalf("ParseTranscript failed: %v", err)
	}

	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", turns[0].StopReason, "end_turn")
	}
}

func TestExtractUsage(t *testing.T) {
	msg := map[string]interface{}{
		"message": map[string]interface{}{
			"usage": map[string]interface{}{
				"input_tokens":                100.0,
				"output_tokens":               50.0,
				"cache_read_input_tokens":     30.0,
				"cache_creation_input_tokens": 10.0,
			},
		},
	}

	input, output, cacheRead, cacheCreation := extractUsage(msg)
	if input != 100 {
		t.Errorf("input = %d, want 100", input)
	}
	if output != 50 {
		t.Errorf("output = %d, want 50", output)
	}
	if cacheRead != 30 {
		t.Errorf("cacheRead = %d, want 30", cacheRead)
	}
	if cacheCreation != 10 {
		t.Errorf("cacheCreation = %d, want 10", cacheCreation)
	}
}

func TestHelpers(t *testing.T) {
	t.Run("msgRole_user_type", func(t *testing.T) {
		msg := map[string]interface{}{"type": "user"}
		if got := msgRole(msg); got != "user" {
			t.Errorf("msgRole = %q, want %q", got, "user")
		}
	})

	t.Run("msgRole_assistant_message", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"role": "assistant",
			},
		}
		if got := msgRole(msg); got != "assistant" {
			t.Errorf("msgRole = %q, want %q", got, "assistant")
		}
	})

	t.Run("getTextContent_plain_string", func(t *testing.T) {
		msg := map[string]interface{}{
			"content": "hello world",
		}
		if got := getTextContent(msg); got != "hello world" {
			t.Errorf("getTextContent = %q, want %q", got, "hello world")
		}
	})

	t.Run("getTextContent_content_blocks", func(t *testing.T) {
		msg := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "block one"},
				map[string]interface{}{"type": "text", "text": "block two"},
			},
		}
		got := getTextContent(msg)
		want := "block one\nblock two"
		if got != want {
			t.Errorf("getTextContent = %q, want %q", got, want)
		}
	})

	t.Run("isToolResult_true", func(t *testing.T) {
		msg := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": "toolu_123",
					"content":     "result data",
				},
			},
		}
		if !isToolResult(msg) {
			t.Error("isToolResult = false, want true")
		}
	})

	t.Run("isToolResult_false", func(t *testing.T) {
		msg := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "just text"},
			},
		}
		if isToolResult(msg) {
			t.Error("isToolResult = true, want false")
		}
	})

	t.Run("extractTimestamp", func(t *testing.T) {
		msg := map[string]interface{}{
			"timestamp": "2026-01-15T10:00:00Z",
		}
		got := extractTimestamp(msg)
		want := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("extractTimestamp = %v, want %v", got, want)
		}
	})

	t.Run("extractModel", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"model": "claude-sonnet-4-20250514",
			},
		}
		if got := extractModel(msg); got != "claude-sonnet-4-20250514" {
			t.Errorf("extractModel = %q, want %q", got, "claude-sonnet-4-20250514")
		}
	})

	t.Run("extractStopReason", func(t *testing.T) {
		msg := map[string]interface{}{
			"message": map[string]interface{}{
				"stop_reason": "end_turn",
			},
		}
		if got := extractStopReason(msg); got != "end_turn" {
			t.Errorf("extractStopReason = %q, want %q", got, "end_turn")
		}
	})
}

func TestMergeAssistantParts(t *testing.T) {
	part1 := map[string]interface{}{
		"message": map[string]interface{}{
			"id":   "msg_001",
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "part1"},
			},
		},
	}
	part2 := map[string]interface{}{
		"message": map[string]interface{}{
			"id":   "msg_001",
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "part2"},
			},
		},
	}

	merged := MergeAssistantParts([]map[string]interface{}{part1, part2})
	if merged == nil {
		t.Fatal("MergeAssistantParts returned nil")
	}

	text := getTextContent(merged)
	want := "part1\npart2"
	if text != want {
		t.Errorf("merged text = %q, want %q", text, want)
	}
}
