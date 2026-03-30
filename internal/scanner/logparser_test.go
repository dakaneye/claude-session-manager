package scanner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLog(t *testing.T) {
	logPath := filepath.Join("..", "..", "testdata", "sandbox", "logs", "2026-03-27-abc123.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	summary := ParseLog(data)

	t.Run("total tool count", func(t *testing.T) {
		if summary.TotalTools != 6 {
			t.Errorf("TotalTools = %d, want 6", summary.TotalTools)
		}
	})

	t.Run("tool counts by name", func(t *testing.T) {
		want := map[string]int{"Read": 1, "Edit": 2, "Bash": 3}
		for tool, count := range want {
			if summary.ToolCounts[tool] != count {
				t.Errorf("ToolCounts[%s] = %d, want %d", tool, summary.ToolCounts[tool], count)
			}
		}
	})

	t.Run("last tool", func(t *testing.T) {
		if summary.LastTool != "Bash" {
			t.Errorf("LastTool = %s, want Bash", summary.LastTool)
		}
	})

	t.Run("last activity time", func(t *testing.T) {
		want, _ := time.Parse(time.RFC3339Nano, "2026-03-27T14:34:00.000Z")
		if !summary.LastActivity.Equal(want) {
			t.Errorf("LastActivity = %v, want %v", summary.LastActivity, want)
		}
	})

	t.Run("recent activity entries", func(t *testing.T) {
		if len(summary.RecentActivity) == 0 {
			t.Fatal("RecentActivity is empty")
		}
		last := summary.RecentActivity[len(summary.RecentActivity)-1]
		if last.Tool != "Bash" {
			t.Errorf("last activity tool = %s, want Bash", last.Tool)
		}
	})

	t.Run("failed tool results", func(t *testing.T) {
		if summary.FailedToolResults != 2 {
			t.Errorf("FailedToolResults = %d, want 2", summary.FailedToolResults)
		}
	})
}
