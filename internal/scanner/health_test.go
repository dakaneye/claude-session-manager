package scanner

import (
	"testing"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestEvaluateHealth(t *testing.T) {
	now := time.Now()

	t.Run("repeated file edits triggers warning", func(t *testing.T) {
		activity := []ActivityEntry{
			{Time: now.Add(-4 * time.Minute), Tool: "Edit", Detail: "auth.go"},
			{Time: now.Add(-3 * time.Minute), Tool: "Edit", Detail: "auth.go"},
			{Time: now.Add(-2 * time.Minute), Tool: "Edit", Detail: "auth.go"},
			{Time: now.Add(-1 * time.Minute), Tool: "Edit", Detail: "auth.go"},
			{Time: now, Tool: "Edit", Detail: "auth.go"},
		}
		diagnostics := EvaluateHealth(activity, now)
		found := findDiagnostic(diagnostics, "repeated-edit")
		if found == nil {
			t.Fatal("expected repeated-edit diagnostic, got none")
		}
		if found.Severity != session.SeverityWarning {
			t.Errorf("severity = %s, want warning", found.Severity)
		}
	})

	t.Run("8 edits on same file triggers critical", func(t *testing.T) {
		var activity []ActivityEntry
		for i := 0; i < 8; i++ {
			activity = append(activity, ActivityEntry{
				Time:   now.Add(time.Duration(-8+i) * time.Minute),
				Tool:   "Edit",
				Detail: "auth.go",
			})
		}
		diagnostics := EvaluateHealth(activity, now)
		found := findDiagnostic(diagnostics, "repeated-edit")
		if found == nil {
			t.Fatal("expected repeated-edit diagnostic")
		}
		if found.Severity != session.SeverityCritical {
			t.Errorf("severity = %s, want critical", found.Severity)
		}
	})

	t.Run("consecutive test failures triggers critical", func(t *testing.T) {
		activity := []ActivityEntry{
			{Time: now.Add(-3 * time.Minute), Tool: "Bash", Detail: "go test ./...", IsError: true},
			{Time: now.Add(-2 * time.Minute), Tool: "Bash", Detail: "go test ./...", IsError: true},
			{Time: now.Add(-1 * time.Minute), Tool: "Bash", Detail: "go test ./...", IsError: true},
		}
		diagnostics := EvaluateHealth(activity, now)
		found := findDiagnostic(diagnostics, "test-loop")
		if found == nil {
			t.Fatal("expected test-loop diagnostic")
		}
		if found.Severity != session.SeverityCritical {
			t.Errorf("severity = %s, want critical", found.Severity)
		}
	})

	t.Run("no activity for 5 min triggers warning", func(t *testing.T) {
		activity := []ActivityEntry{
			{Time: now.Add(-6 * time.Minute), Tool: "Edit", Detail: "main.go"},
		}
		diagnostics := EvaluateHealth(activity, now)
		found := findDiagnostic(diagnostics, "idle")
		if found == nil {
			t.Fatal("expected idle diagnostic")
		}
		if found.Severity != session.SeverityWarning {
			t.Errorf("severity = %s, want warning", found.Severity)
		}
	})

	t.Run("no activity for 10 min triggers critical", func(t *testing.T) {
		activity := []ActivityEntry{
			{Time: now.Add(-11 * time.Minute), Tool: "Edit", Detail: "main.go"},
		}
		diagnostics := EvaluateHealth(activity, now)
		found := findDiagnostic(diagnostics, "idle")
		if found == nil {
			t.Fatal("expected idle diagnostic")
		}
		if found.Severity != session.SeverityCritical {
			t.Errorf("severity = %s, want critical", found.Severity)
		}
	})

	t.Run("repeated identical bash command triggers warning", func(t *testing.T) {
		activity := []ActivityEntry{
			{Time: now.Add(-3 * time.Minute), Tool: "Bash", Detail: "cat /dev/null"},
			{Time: now.Add(-2 * time.Minute), Tool: "Bash", Detail: "cat /dev/null"},
			{Time: now.Add(-1 * time.Minute), Tool: "Bash", Detail: "cat /dev/null"},
		}
		diagnostics := EvaluateHealth(activity, now)
		found := findDiagnostic(diagnostics, "repeated-command")
		if found == nil {
			t.Fatal("expected repeated-command diagnostic")
		}
	})

	t.Run("healthy session has no diagnostics", func(t *testing.T) {
		activity := []ActivityEntry{
			{Time: now.Add(-2 * time.Minute), Tool: "Read", Detail: "main.go"},
			{Time: now.Add(-1 * time.Minute), Tool: "Edit", Detail: "main.go"},
			{Time: now, Tool: "Bash", Detail: "go test ./..."},
		}
		diagnostics := EvaluateHealth(activity, now)
		if len(diagnostics) != 0 {
			t.Errorf("expected no diagnostics, got %d: %+v", len(diagnostics), diagnostics)
		}
	})
}

func TestCheckContextWindow(t *testing.T) {
	t.Run("65% usage triggers warning", func(t *testing.T) {
		diagnostics := CheckContextWindow(0.65)
		found := findDiagnostic(diagnostics, "high-context")
		if found == nil {
			t.Fatal("expected high-context diagnostic")
		}
		if found.Severity != session.SeverityWarning {
			t.Errorf("severity = %s, want warning", found.Severity)
		}
	})

	t.Run("75% usage triggers critical", func(t *testing.T) {
		diagnostics := CheckContextWindow(0.75)
		found := findDiagnostic(diagnostics, "high-context")
		if found == nil {
			t.Fatal("expected high-context diagnostic")
		}
		if found.Severity != session.SeverityCritical {
			t.Errorf("severity = %s, want critical", found.Severity)
		}
	})

	t.Run("50% usage is healthy", func(t *testing.T) {
		diagnostics := CheckContextWindow(0.50)
		if len(diagnostics) != 0 {
			t.Errorf("expected no diagnostics, got %+v", diagnostics)
		}
	})
}

func findDiagnostic(diagnostics []session.Diagnostic, signal string) *session.Diagnostic {
	for _, d := range diagnostics {
		if d.Signal == signal {
			return &d
		}
	}
	return nil
}
