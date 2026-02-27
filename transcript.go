package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// msgRole returns "user", "assistant", or other type from a transcript line.
func msgRole(msg map[string]interface{}) string {
	if t, ok := msg["type"].(string); ok && t != "" {
		return t
	}
	if inner, ok := msg["message"]; ok {
		if innerMap, ok := inner.(map[string]interface{}); ok {
			if r, ok := innerMap["role"].(string); ok {
				return r
			}
		}
	}
	return ""
}

// assistantMsgID extracts the message ID for merging streamed assistant parts.
func assistantMsgID(msg map[string]interface{}) string {
	if inner, ok := msg["message"]; ok {
		if innerMap, ok := inner.(map[string]interface{}); ok {
			if id, ok := innerMap["id"].(string); ok {
				return id
			}
		}
	}
	return ""
}

// getContent extracts content from a message (handles nested {message:{content:...}}).
func getContent(msg map[string]interface{}) interface{} {
	if inner, ok := msg["message"]; ok {
		if innerMap, ok := inner.(map[string]interface{}); ok {
			return innerMap["content"]
		}
	}
	return msg["content"]
}

// getTextContent extracts concatenated text from a message's content blocks.
func getTextContent(msg map[string]interface{}) string {
	content := getContent(msg)
	if s, ok := content.(string); ok {
		return s
	}
	list, ok := content.([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range list {
		switch v := item.(type) {
		case map[string]interface{}:
			if v["type"] == "text" {
				if t, ok := v["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		case string:
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, "\n")
}

// getToolCalls returns all tool_use blocks from a message.
func getToolCalls(msg map[string]interface{}) []map[string]interface{} {
	content := getContent(msg)
	list, ok := content.([]interface{})
	if !ok {
		return nil
	}
	var calls []map[string]interface{}
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if ok && m["type"] == "tool_use" {
			calls = append(calls, m)
		}
	}
	return calls
}

// isToolResult checks whether a user message contains tool_result blocks.
func isToolResult(msg map[string]interface{}) bool {
	content := getContent(msg)
	list, ok := content.([]interface{})
	if !ok {
		return false
	}
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if ok && m["type"] == "tool_result" {
			return true
		}
	}
	return false
}

// mergeAssistantParts combines multiple streamed assistant message parts into one.
func mergeAssistantParts(parts []map[string]interface{}) map[string]interface{} {
	if len(parts) == 0 {
		return nil
	}
	var merged []interface{}
	for _, part := range parts {
		content := getContent(part)
		if list, ok := content.([]interface{}); ok {
			merged = append(merged, list...)
		} else if content != nil {
			merged = append(merged, map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("%v", content),
			})
		}
	}
	result := shallowCopyMap(parts[0])
	if inner, ok := result["message"]; ok {
		if innerMap, ok := inner.(map[string]interface{}); ok {
			cp := shallowCopyMap(innerMap)
			cp["content"] = merged
			result["message"] = cp
		}
	} else {
		result["content"] = merged
	}
	return result
}

func shallowCopyMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// extractTimestamp parses the timestamp from a transcript line.
func extractTimestamp(msg map[string]interface{}) time.Time {
	if ts, ok := msg["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			return t
		}
	}
	if ts, ok := msg["timestamp"].(float64); ok {
		return time.UnixMilli(int64(ts))
	}
	return time.Now()
}

// extractModel gets the model name from an assistant message.
func extractModel(msg map[string]interface{}) string {
	if inner, ok := msg["message"]; ok {
		if innerMap, ok := inner.(map[string]interface{}); ok {
			if m, ok := innerMap["model"].(string); ok {
				return m
			}
		}
	}
	return ""
}

// extractUsage gets token counts from an assistant message's usage field.
func extractUsage(msg map[string]interface{}) (input, output, cacheRead, cacheCreation int) {
	var usage map[string]interface{}
	if inner, ok := msg["message"]; ok {
		if innerMap, ok := inner.(map[string]interface{}); ok {
			if u, ok := innerMap["usage"].(map[string]interface{}); ok {
				usage = u
			}
		}
	}
	if usage == nil {
		return
	}
	if v, ok := usage["input_tokens"].(float64); ok {
		input = int(v)
	}
	if v, ok := usage["output_tokens"].(float64); ok {
		output = int(v)
	}
	if v, ok := usage["cache_read_input_tokens"].(float64); ok {
		cacheRead = int(v)
	}
	if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
		cacheCreation = int(v)
	}
	return
}

// parseTranscript reads a JSONL transcript from startLine and returns parsed turns.
func parseTranscript(transcriptPath string, startLine int) ([]Turn, int, error) {
	data, err := os.ReadFile(transcriptPath)
	if err != nil {
		return nil, startLine, fmt.Errorf("read transcript: %w", err)
	}
	raw := strings.TrimRight(string(data), "\n")
	if raw == "" {
		return nil, startLine, nil
	}
	lines := strings.Split(raw, "\n")
	totalLines := len(lines)

	if startLine >= totalLines {
		return nil, startLine, nil
	}

	var newMessages []map[string]interface{}
	for i := startLine; i < totalLines; i++ {
		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(lines[i]), &msg); err != nil {
			continue
		}
		newMessages = append(newMessages, msg)
	}
	if len(newMessages) == 0 {
		return nil, totalLines, nil
	}

	var turns []Turn
	var currentUser map[string]interface{}
	var currentAssistants []map[string]interface{}
	var currentAssistantParts []map[string]interface{}
	var currentMsgID string
	var currentToolResults []map[string]interface{}

	finalizeParts := func() {
		if currentMsgID != "" && len(currentAssistantParts) > 0 {
			merged := mergeAssistantParts(currentAssistantParts)
			if merged != nil {
				currentAssistants = append(currentAssistants, merged)
			}
			currentAssistantParts = nil
			currentMsgID = ""
		}
	}

	finalizeTurn := func(turnNum int) {
		finalizeParts()
		if currentUser == nil || len(currentAssistants) == 0 {
			return
		}

		userText := getTextContent(currentUser)
		userTS := extractTimestamp(currentUser)

		var model string
		var inputTok, outputTok, cacheReadTok, cacheCreationTok int
		for _, am := range currentAssistants {
			if m := extractModel(am); m != "" {
				model = m
			}
			in, out, cr, cc := extractUsage(am)
			inputTok += in
			outputTok += out
			cacheReadTok += cr
			cacheCreationTok += cc
		}

		var toolCalls []ToolCall
		for _, am := range currentAssistants {
			amTS := extractTimestamp(am)
			calls := getToolCalls(am)
			for _, tc := range calls {
				name, _ := tc["name"].(string)
				id, _ := tc["id"].(string)
				tcall := ToolCall{
					Name:      name,
					ID:        id,
					Input:     tc["input"],
					StartTime: amTS,
					Success:   true,
				}
				// Match with tool results.
				for _, tr := range currentToolResults {
					trContent := getContent(tr)
					if list, ok := trContent.([]interface{}); ok {
						for _, item := range list {
							if m, ok := item.(map[string]interface{}); ok {
								if m["tool_use_id"] == id {
									tcall.Output = m["content"]
									tcall.EndTime = extractTimestamp(tr)
									if isErr, ok := m["is_error"].(bool); ok && isErr {
										tcall.Success = false
									}
								}
							}
						}
					}
				}
				if tcall.EndTime.IsZero() {
					tcall.EndTime = amTS
				}
				toolCalls = append(toolCalls, tcall)
			}
		}

		endTS := extractTimestamp(currentAssistants[len(currentAssistants)-1])
		if len(toolCalls) > 0 {
			lastTool := toolCalls[len(toolCalls)-1]
			if lastTool.EndTime.After(endTS) {
				endTS = lastTool.EndTime
			}
		}

		turns = append(turns, Turn{
			Number:              turnNum,
			UserText:            userText,
			UserTimestamp:        userTS,
			AssistantMessages:   currentAssistants,
			ToolCalls:           toolCalls,
			Model:               model,
			InputTokens:         inputTok,
			OutputTokens:        outputTok,
			CacheReadTokens:     cacheReadTok,
			CacheCreationTokens: cacheCreationTok,
			StartTime:           userTS,
			EndTime:             endTS,
		})
	}

	turnNum := 0
	for _, msg := range newMessages {
		role := msgRole(msg)
		switch role {
		case "user":
			if isToolResult(msg) {
				currentToolResults = append(currentToolResults, msg)
				continue
			}
			turnNum++
			finalizeTurn(turnNum - 1)
			currentUser = msg
			currentAssistants = nil
			currentAssistantParts = nil
			currentMsgID = ""
			currentToolResults = nil

		case "assistant":
			msgID := assistantMsgID(msg)
			if msgID == "" {
				currentAssistantParts = append(currentAssistantParts, msg)
			} else if msgID == currentMsgID {
				currentAssistantParts = append(currentAssistantParts, msg)
			} else {
				finalizeParts()
				currentMsgID = msgID
				currentAssistantParts = []map[string]interface{}{msg}
			}
		}
	}

	// Finalize last turn.
	turnNum++
	finalizeTurn(turnNum)

	// Remove empty first turn if the first message wasn't a user turn.
	var filtered []Turn
	for _, t := range turns {
		if t.Number > 0 {
			filtered = append(filtered, t)
		}
	}

	return filtered, totalLines, nil
}
