package scanner

import (
	"fmt"
	"strings"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// EvaluateHealth runs all heuristics against session activity and returns diagnostics.
func EvaluateHealth(activity []ActivityEntry, now time.Time) []session.Diagnostic {
	var diagnostics []session.Diagnostic
	diagnostics = append(diagnostics, checkRepeatedEdits(activity)...)
	diagnostics = append(diagnostics, checkTestLoop(activity)...)
	diagnostics = append(diagnostics, checkIdle(activity, now)...)
	diagnostics = append(diagnostics, checkRepeatedCommand(activity)...)
	return diagnostics
}

// CheckContextWindow checks if context window usage is too high.
func CheckContextWindow(usage float64) []session.Diagnostic {
	if usage >= 0.75 {
		return []session.Diagnostic{{
			Signal:   "high-context",
			Severity: session.SeverityCritical,
			Detail:   fmt.Sprintf("context window %.0f%% used", usage*100),
		}}
	}
	if usage >= 0.65 {
		return []session.Diagnostic{{
			Signal:   "high-context",
			Severity: session.SeverityWarning,
			Detail:   fmt.Sprintf("context window %.0f%% used", usage*100),
		}}
	}
	return nil
}

func checkRepeatedEdits(activity []ActivityEntry) []session.Diagnostic {
	fileCounts := make(map[string]int)
	for _, a := range activity {
		if a.Tool == "Edit" && a.Detail != "" {
			fileCounts[a.Detail]++
		}
	}
	var diagnostics []session.Diagnostic
	for file, count := range fileCounts {
		if count >= 8 {
			diagnostics = append(diagnostics, session.Diagnostic{
				Signal:   "repeated-edit",
				Severity: session.SeverityCritical,
				Detail:   fmt.Sprintf("%s edited %d times", file, count),
			})
		} else if count >= 5 {
			diagnostics = append(diagnostics, session.Diagnostic{
				Signal:   "repeated-edit",
				Severity: session.SeverityWarning,
				Detail:   fmt.Sprintf("%s edited %d times", file, count),
			})
		}
	}
	return diagnostics
}

func checkTestLoop(activity []ActivityEntry) []session.Diagnostic {
	consecutive := 0
	for i := len(activity) - 1; i >= 0; i-- {
		a := activity[i]
		if !isTestCommand(a) {
			break
		}
		if a.IsError {
			consecutive++
		} else {
			break
		}
	}
	if consecutive >= 3 {
		return []session.Diagnostic{{
			Signal:   "test-loop",
			Severity: session.SeverityCritical,
			Detail:   fmt.Sprintf("tests failing %d times consecutively", consecutive),
		}}
	}
	return nil
}

func isTestCommand(a ActivityEntry) bool {
	if a.Tool != "Bash" {
		return false
	}
	d := strings.ToLower(a.Detail)
	return strings.Contains(d, "test") || strings.Contains(d, "pytest") || strings.Contains(d, "jest")
}

func checkIdle(activity []ActivityEntry, now time.Time) []session.Diagnostic {
	if len(activity) == 0 {
		return nil
	}
	last := activity[len(activity)-1]
	if last.Time.IsZero() {
		return nil
	}
	idle := now.Sub(last.Time)
	if idle >= 10*time.Minute {
		return []session.Diagnostic{{
			Signal:   "idle",
			Severity: session.SeverityCritical,
			Detail:   fmt.Sprintf("no activity for %d minutes", int(idle.Minutes())),
		}}
	}
	if idle >= 5*time.Minute {
		return []session.Diagnostic{{
			Signal:   "idle",
			Severity: session.SeverityWarning,
			Detail:   fmt.Sprintf("no activity for %d minutes", int(idle.Minutes())),
		}}
	}
	return nil
}

func checkRepeatedCommand(activity []ActivityEntry) []session.Diagnostic {
	if len(activity) < 3 {
		return nil
	}
	tail := activity[len(activity)-3:]
	if tail[0].Tool != "Bash" || tail[0].Detail == "" {
		return nil
	}
	cmd := tail[0].Detail
	for _, a := range tail[1:] {
		if a.Tool != "Bash" || a.Detail != cmd {
			return nil
		}
	}
	return []session.Diagnostic{{
		Signal:   "repeated-command",
		Severity: session.SeverityWarning,
		Detail:   fmt.Sprintf("identical command run 3 times: %s", cmd),
	}}
}
