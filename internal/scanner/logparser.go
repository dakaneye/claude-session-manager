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
}

type logMessage struct {
	Content []logContent `json:"content"`
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
			for _, c := range event.Message.Content {
				if c.Type == "tool_use" && c.Name != "" {
					summary.TotalTools++
					summary.ToolCounts[c.Name]++
					pendingTool = c.Name
					pendingDetail = parseToolDetail(c.Name, c.Input)
				}
				if c.Type == "text" && c.Text != "" {
					summary.LastMessage = c.Text
					summary.ConversationTail = append(summary.ConversationTail, c.Text)
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

			isError := false
			for _, c := range event.Message.Content {
				if c.Type == "tool_result" && c.IsError {
					isError = true
					summary.FailedToolResults++
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
