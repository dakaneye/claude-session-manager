package scanner

import (
	"encoding/json"
	"strings"
	"time"
)

// LogSummary is the parsed result of a stream-JSON session log.
type LogSummary struct {
	TotalTools        int
	ToolCounts        map[string]int
	LastTool          string
	LastActivity      time.Time
	RecentActivity    []ActivityEntry
	FailedToolResults int
	LastMessage       string // Last assistant text content
	// ConversationTail holds the last N assistant text messages for peek mode.
	ConversationTail []string
}

const maxConversationTail = 20

// ActivityEntry is a single tool invocation extracted from the log.
type ActivityEntry struct {
	Time    time.Time
	Tool    string
	Detail  string
	IsError bool
}

type logEvent struct {
	Type      string      `json:"type"`
	Subtype   string      `json:"subtype"`
	Message   *logMessage `json:"message"`
	Timestamp string      `json:"timestamp"`
	PromptID  string      `json:"promptId"`
}

type logMessage struct {
	Content json.RawMessage `json:"content"`
}

// parseContent handles the content array which can contain both objects and strings.
func parseContent(raw json.RawMessage) []logContent {
	if len(raw) == 0 {
		return nil
	}
	var contents []logContent
	if err := json.Unmarshal(raw, &contents); err == nil {
		return contents
	}
	// Try as array of mixed types (strings and objects).
	var mixed []json.RawMessage
	if err := json.Unmarshal(raw, &mixed); err != nil {
		return nil
	}
	for _, item := range mixed {
		var c logContent
		if err := json.Unmarshal(item, &c); err == nil {
			contents = append(contents, c)
			continue
		}
		// Try as plain string.
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			contents = append(contents, logContent{Type: "text", Text: s})
		}
	}
	return contents
}

type logContent struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content string          `json:"content"`
	Text    string          `json:"text"`
	IsError bool            `json:"is_error"`
}

type toolInput struct {
	FilePath    string `json:"file_path"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// ParseLog parses raw stream-JSON log bytes into a summary.
func ParseLog(data []byte) LogSummary {
	summary := LogSummary{
		ToolCounts: make(map[string]int),
	}

	var lastToolTime time.Time
	var lastToolName string
	var pendingTool string
	var pendingDetail string
	var pendingTime time.Time

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event logEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		switch event.Type {
		case "assistant":
			if event.Message == nil {
				continue
			}
			contents := parseContent(event.Message.Content)
			for _, c := range contents {
				if c.Type == "tool_use" && c.Name != "" {
					summary.TotalTools++
					summary.ToolCounts[c.Name]++
					pendingTool = c.Name
					pendingDetail = parseToolDetail(c.Name, c.Input)
				}
				if c.Type == "text" && c.Text != "" {
					summary.LastMessage = c.Text
					summary.ConversationTail = append(summary.ConversationTail, "Claude: "+c.Text)
					if len(summary.ConversationTail) > maxConversationTail {
						summary.ConversationTail = summary.ConversationTail[1:]
					}
				}
			}

		case "user":
			ts := parseTimestamp(event.Timestamp)
			if event.Message == nil {
				continue
			}

			contents := parseContent(event.Message.Content)
			isError := false
			for _, c := range contents {
				if c.Type == "tool_result" && c.IsError {
					isError = true
					summary.FailedToolResults++
				}
			}

			// Capture user text for conversation tail.
			// Include messages that have text content but aren't tool results or system noise.
			userText := extractUserText(event.Message.Content, contents)
			if userText != "" && !isSystemMessage(userText) && !isToolResultOnly(contents) {
				summary.ConversationTail = append(summary.ConversationTail, "You: "+userText)
				if len(summary.ConversationTail) > maxConversationTail {
					summary.ConversationTail = summary.ConversationTail[1:]
				}
			}

			if pendingTool != "" {
				if !ts.IsZero() {
					pendingTime = ts
				}
				entry := ActivityEntry{
					Time:    pendingTime,
					Tool:    pendingTool,
					Detail:  pendingDetail,
					IsError: isError,
				}
				summary.RecentActivity = append(summary.RecentActivity, entry)
				if !pendingTime.IsZero() && pendingTime.After(lastToolTime) {
					lastToolTime = pendingTime
					lastToolName = pendingTool
				}
				pendingTool = ""
				pendingDetail = ""
			}
		}
	}

	summary.LastTool = lastToolName
	summary.LastActivity = lastToolTime

	return summary
}

// isToolResultOnly returns true if the content only contains tool_result entries.
func isToolResultOnly(contents []logContent) bool {
	if len(contents) == 0 {
		return false
	}
	for _, c := range contents {
		if c.Type != "tool_result" {
			return false
		}
	}
	return true
}

// isSystemMessage detects auto-generated system messages that shouldn't
// appear in conversation (task notifications, hook output, skill loading).
func isSystemMessage(text string) bool {
	return strings.HasPrefix(text, "<task-notification>") ||
		strings.HasPrefix(text, "<system-reminder>") ||
		strings.HasPrefix(text, "Base directory for this skill:") ||
		strings.HasPrefix(text, "Tool loaded.")
}

// extractUserText gets the user's text from a message content field.
// Content can be a JSON string, an array of objects, or an array with strings.
func extractUserText(raw json.RawMessage, parsed []logContent) string {
	// First try: check if content is a plain string (common for user prompts).
	var topStr string
	if err := json.Unmarshal(raw, &topStr); err == nil && len(topStr) > 1 {
		return topStr
	}
	// Second try: look for text objects in parsed content.
	for _, c := range parsed {
		if c.Type == "text" && c.Text != "" {
			return c.Text
		}
	}
	return ""
}

func parseToolDetail(tool string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var input toolInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return ""
	}
	switch tool {
	case "Bash":
		if input.Description != "" {
			return input.Description
		}
		return input.Command
	case "Read", "Edit", "Write":
		return input.FilePath
	default:
		if input.Description != "" {
			return input.Description
		}
		return ""
	}
}

func parseTimestamp(ts string) time.Time {
	if ts == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}
	}
	return t
}
