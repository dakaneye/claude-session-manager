# Claude Session Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `cs`, a Go TUI + CLI for managing multiple Claude Code sessions (interactive and autonomous sandbox) with health heuristic anomaly detection.

**Architecture:** Two-layer design — scanner layer reads session state from native Claude (`~/.claude/sessions/`) and claude-sandbox (`.claude-sandbox/sessions/`) with health heuristics, TUI layer renders a split-pane dashboard using Bubbletea v2. CLI subcommands share the scanner for scripting.

**Tech Stack:** Go, Bubbletea v2, Bubbles v2, Lipgloss v2, Cobra

**Spec:** `docs/superpowers/specs/2026-03-30-claude-session-manager-design.md`

---

## File Structure

```
claude-session-manager/
  cmd/cs/
    main.go                   # Cobra root + TUI default, version injection
    ls.go                     # cs ls subcommand
    peek.go                   # cs peek subcommand
    stop.go                   # cs stop subcommand
    label.go                  # cs label subcommand
    clean.go                  # cs clean subcommand
    new.go                    # cs new subcommand
  internal/
    session/
      session.go              # Session, Diagnostic, Source, Status, Health types
    scanner/
      scanner.go              # Scanner orchestrator, SessionSource interface
      sandbox.go              # Sandbox session source
      native.go               # Native Claude session source
      health.go               # Health heuristic engine
      logparser.go            # Stream-JSON log parsing
      scanner_test.go         # Scanner orchestrator tests
      sandbox_test.go         # Sandbox scanner tests
      native_test.go          # Native scanner tests
      health_test.go          # Health heuristic tests
      logparser_test.go       # Log parser tests
    tui/
      app.go                  # Root Bubbletea model, tick, layout
      sessions.go             # Left pane: session list
      detail.go               # Right pane: detail + peek viewport
      statusbar.go            # Bottom bar
      styles.go               # All Lipgloss styles
      keys.go                 # Keybinding definitions
      app_test.go             # TUI state transition tests
  testdata/
    sandbox/
      sessions/
        2026-03-27-abc123.json
        my-feature.json        # symlink to above
      logs/
        2026-03-27-abc123.log
    native/
      sessions/
        12345.json
  Makefile
  .goreleaser.yml
  .golangci.yml
  .pre-commit-config.yaml
  .gitignore
```

---

### Task 1: Project Scaffolding

**Files:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `.goreleaser.yml`
- Create: `.golangci.yml`
- Create: `.pre-commit-config.yaml`
- Create: `.gitignore`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/samueldacanay/dev/personal/claude-session-manager
go mod init github.com/dakaneye/claude-session-manager
```

- [ ] **Step 2: Create Makefile**

```makefile
BINARY=cs
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

.PHONY: build test lint tidy verify clean

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/cs

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy
	@git diff --exit-code go.mod go.sum || (echo "go.mod/go.sum not tidy" && exit 1)

vet:
	go vet ./...

verify: build vet lint test tidy
	@echo "All checks passed"

clean:
	rm -rf bin/

install: build
	cp bin/$(BINARY) $(GOPATH)/bin/$(BINARY)
```

- [ ] **Step 3: Create .gitignore**

```
bin/
dist/
*.exe
*.test
*.out
.DS_Store
```

- [ ] **Step 4: Create .golangci.yml**

```yaml
run:
  timeout: 5m

linters:
  enable:
    - errcheck
    - govet
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    - typecheck
    - misspell
    - gofmt
    - revive

linters-settings:
  revive:
    rules:
      - name: exported
        disabled: true
```

- [ ] **Step 5: Create .goreleaser.yml**

```yaml
version: 2

builds:
  - main: ./cmd/cs
    binary: cs
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
      - linux
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Os }}_{{ .Arch }}"

brews:
  - repository:
      owner: dakaneye
      name: homebrew-tap
    homepage: https://github.com/dakaneye/claude-session-manager
    description: TUI + CLI for managing multiple Claude Code sessions
    license: Apache-2.0

checksum:
  name_template: checksums.txt

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"
```

- [ ] **Step 6: Create .pre-commit-config.yaml**

```yaml
repos:
  - repo: local
    hooks:
      - id: go-build
        name: go build
        entry: make build
        language: system
        pass_filenames: false
      - id: golangci-lint
        name: golangci-lint
        entry: golangci-lint run ./...
        language: system
        pass_filenames: false
      - id: go-mod-tidy
        name: go mod tidy
        entry: bash -c 'go mod tidy && git diff --exit-code go.mod go.sum'
        language: system
        pass_filenames: false
```

- [ ] **Step 7: Create placeholder main.go so the module compiles**

Create `cmd/cs/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "cs %s\n", version)
}
```

- [ ] **Step 8: Verify the module compiles**

```bash
go build ./cmd/cs
```

Expected: succeeds, produces `cs` binary.

- [ ] **Step 9: Commit**

```bash
git add go.mod Makefile .gitignore .golangci.yml .goreleaser.yml .pre-commit-config.yaml cmd/
git commit -m "chore: scaffold project with go module, makefile, and CI configs"
```

---

### Task 2: Session Types

**Files:**
- Create: `internal/session/session.go`

- [ ] **Step 1: Define the session types**

Create `internal/session/session.go`:

```go
package session

import "time"

// Source identifies where a session was discovered.
type Source string

const (
	SourceSandbox Source = "sandbox"
	SourceNative  Source = "native"
)

// Status represents the current state of a session.
type Status string

const (
	StatusRunning Status = "running"
	StatusIdle    Status = "idle"
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusBlocked Status = "blocked"
	// Sandbox-specific statuses mapped from claude-sandbox state.
	StatusSpeccing Status = "speccing"
	StatusReady    Status = "ready"
)

// Health is a traffic-light indicator for session health.
type Health string

const (
	HealthGreen  Health = "green"
	HealthYellow Health = "yellow"
	HealthRed    Health = "red"
)

// Severity indicates how urgent a diagnostic is.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Diagnostic describes a single health signal detected in a session.
type Diagnostic struct {
	Signal   string   `json:"signal"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
}

// Session is the unified model for both native and sandbox sessions.
type Session struct {
	ID           string       `json:"id"`
	Name         string       `json:"name,omitempty"`
	Source       Source       `json:"source"`
	Status       Status       `json:"status"`
	Health       Health       `json:"health"`
	Dir          string       `json:"dir"`
	Branch       string       `json:"branch,omitempty"`
	StartedAt    time.Time    `json:"started_at"`
	LastActivity time.Time    `json:"last_activity"`
	Task         string       `json:"task,omitempty"`
	Diagnostics  []Diagnostic `json:"diagnostics,omitempty"`
	// PID is set for native sessions to check liveness.
	PID int `json:"pid,omitempty"`
	// LogPath is set for sandbox sessions for peeking.
	LogPath string `json:"log_path,omitempty"`
}

// WorstHealth returns the most severe health from a set of diagnostics.
func WorstHealth(diagnostics []Diagnostic) Health {
	health := HealthGreen
	for _, d := range diagnostics {
		switch d.Severity {
		case SeverityCritical:
			return HealthRed
		case SeverityWarning:
			health = HealthYellow
		}
	}
	return health
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/session/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/session/
git commit -m "feat(session): add unified session types for native and sandbox sources"
```

---

### Task 3: Stream-JSON Log Parser

**Files:**
- Create: `internal/scanner/logparser.go`
- Create: `internal/scanner/logparser_test.go`
- Create: `testdata/sandbox/logs/2026-03-27-abc123.log`

- [ ] **Step 1: Create test fixture from real stream-JSON format**

Create `testdata/sandbox/logs/2026-03-27-abc123.log`:

```json
{"type":"system","subtype":"init","session_id":"abc-123","model":"claude-sonnet-4-6","uuid":"init-1"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/workspace/PLAN.md"}}]},"session_id":"abc-123","uuid":"a-1"}
{"type":"user","message":{"content":[{"type":"tool_result","content":"# Refactor auth middleware"}]},"timestamp":"2026-03-27T14:30:00.000Z","session_id":"abc-123","uuid":"u-1"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/workspace/internal/auth/middleware.go","old_string":"old","new_string":"new"}}]},"session_id":"abc-123","uuid":"a-2"}
{"type":"user","message":{"content":[{"type":"tool_result","content":"OK"}]},"timestamp":"2026-03-27T14:31:00.000Z","session_id":"abc-123","uuid":"u-2"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/workspace/internal/auth/middleware.go","old_string":"old2","new_string":"new2"}}]},"session_id":"abc-123","uuid":"a-3"}
{"type":"user","message":{"content":[{"type":"tool_result","content":"OK"}]},"timestamp":"2026-03-27T14:31:30.000Z","session_id":"abc-123","uuid":"u-3"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./internal/auth/...","description":"Run auth tests"}}]},"session_id":"abc-123","uuid":"a-4"}
{"type":"user","message":{"content":[{"type":"tool_result","content":"FAIL","is_error":true}]},"timestamp":"2026-03-27T14:32:00.000Z","session_id":"abc-123","uuid":"u-4"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./internal/auth/...","description":"Run auth tests"}}]},"session_id":"abc-123","uuid":"a-5"}
{"type":"user","message":{"content":[{"type":"tool_result","content":"FAIL","is_error":true}]},"timestamp":"2026-03-27T14:33:00.000Z","session_id":"abc-123","uuid":"u-5"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./internal/auth/...","description":"Run auth tests"}}]},"session_id":"abc-123","uuid":"a-6"}
{"type":"user","message":{"content":[{"type":"tool_result","content":"PASS"}]},"timestamp":"2026-03-27T14:34:00.000Z","session_id":"abc-123","uuid":"u-6"}
```

- [ ] **Step 2: Write the failing log parser test**

Create `internal/scanner/logparser_test.go`:

```go
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
```

- [ ] **Step 3: Run the test to verify it fails**

```bash
go test ./internal/scanner/ -run TestParseLog -v
```

Expected: FAIL — `ParseLog` not defined.

- [ ] **Step 4: Implement the log parser**

Create `internal/scanner/logparser.go`:

```go
package scanner

import (
	"encoding/json"
	"strings"
	"time"
)

// LogSummary is the parsed result of a stream-JSON session log.
type LogSummary struct {
	TotalTools       int
	ToolCounts       map[string]int
	LastTool         string
	LastActivity     time.Time
	RecentActivity   []ActivityEntry
	FailedToolResults int
}

// ActivityEntry is a single tool invocation extracted from the log.
type ActivityEntry struct {
	Time    time.Time
	Tool    string
	Detail  string
	IsError bool
}

// logEvent mirrors the stream-JSON structure from Claude CLI output.
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
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
go test ./internal/scanner/ -run TestParseLog -v
```

Expected: PASS — all subtests green.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner/logparser.go internal/scanner/logparser_test.go testdata/
git commit -m "feat(scanner): add stream-JSON log parser with tool counting and activity extraction"
```

---

### Task 4: Health Heuristics Engine

**Files:**
- Create: `internal/scanner/health.go`
- Create: `internal/scanner/health_test.go`

- [ ] **Step 1: Write failing health heuristic tests**

Create `internal/scanner/health_test.go`:

```go
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

func findDiagnostic(diagnostics []session.Diagnostic, signal string) *session.Diagnostic {
	for _, d := range diagnostics {
		if d.Signal == signal {
			return &d
		}
	}
	return nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/scanner/ -run TestEvaluateHealth -v
```

Expected: FAIL — `EvaluateHealth` not defined.

- [ ] **Step 3: Implement health heuristics**

Create `internal/scanner/health.go`:

```go
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
	// Check last 3 entries for identical bash commands.
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
```

- [ ] **Step 4: Add context window heuristic test and implementation**

Add to `internal/scanner/health_test.go`:

```go
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
```

Add to `internal/scanner/health.go`:

```go
// CheckContextWindow checks if context window usage is too high.
// usage is a fraction between 0 and 1.
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
```

Note: The sandbox scanner can call `CheckContextWindow` when it has access to the gsd-context-monitor metrics file. The actual file reading is done in the scanner, not the heuristic engine. The heuristic just evaluates the number.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/scanner/ -run TestEvaluateHealth -v
go test ./internal/scanner/ -run TestCheckContextWindow -v
```

Expected: PASS — all subtests green.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner/health.go internal/scanner/health_test.go
git commit -m "feat(scanner): add health heuristic engine with edit, test, idle, command, and context window detection"
```

---

### Task 5: Sandbox Session Scanner

**Files:**
- Create: `internal/scanner/sandbox.go`
- Create: `internal/scanner/sandbox_test.go`
- Create: `testdata/sandbox/sessions/2026-03-27-abc123.json`

- [ ] **Step 1: Create sandbox session fixture**

Create `testdata/sandbox/sessions/2026-03-27-abc123.json`:

```json
{
  "id": "2026-03-27-abc123",
  "name": "auth-refactor",
  "worktree_path": "/tmp/test-repo-sandbox-abc123",
  "branch": "sandbox/2026-03-27-abc123",
  "status": "running",
  "log_path": "",
  "created_at": "2026-03-27T14:00:00Z",
  "started_at": "2026-03-27T14:30:00Z",
  "completed_at": "0001-01-01T00:00:00Z"
}
```

- [ ] **Step 2: Write failing sandbox scanner test**

Create `internal/scanner/sandbox_test.go`:

```go
package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestSandboxSource_Scan(t *testing.T) {
	// Set up a temp dir with sandbox session structure.
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Copy fixture session file.
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "sessions", "2026-03-27-abc123.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "2026-03-27-abc123.json"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	// Copy log fixture to a temp location and patch the session to point to it.
	logFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "logs", "2026-03-27-abc123.log"))
	if err != nil {
		t.Fatalf("read log fixture: %v", err)
	}
	logDir := filepath.Join(tmpDir, "sandbox-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "2026-03-27-abc123.log")
	if err := os.WriteFile(logPath, logFixture, 0o644); err != nil {
		t.Fatal(err)
	}

	src := &SandboxSource{
		RepoPaths: []string{tmpDir},
		LogDir:    logDir,
	}

	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	t.Run("discovers session", func(t *testing.T) {
		if len(sessions) != 1 {
			t.Fatalf("len(sessions) = %d, want 1", len(sessions))
		}
	})

	s := sessions[0]

	t.Run("session fields", func(t *testing.T) {
		if s.ID != "2026-03-27-abc123" {
			t.Errorf("ID = %s, want 2026-03-27-abc123", s.ID)
		}
		if s.Name != "auth-refactor" {
			t.Errorf("Name = %s, want auth-refactor", s.Name)
		}
		if s.Source != session.SourceSandbox {
			t.Errorf("Source = %s, want sandbox", s.Source)
		}
		if s.Status != session.StatusRunning {
			t.Errorf("Status = %s, want running", s.Status)
		}
	})

	t.Run("log parsed for activity", func(t *testing.T) {
		if s.LastActivity.IsZero() {
			t.Error("LastActivity is zero, expected parsed from log")
		}
	})
}

func TestSandboxSource_SkipsSymlinks(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "sessions", "2026-03-27-abc123.json"))
	realFile := filepath.Join(sessDir, "2026-03-27-abc123.json")
	os.WriteFile(realFile, fixture, 0o644)
	os.Symlink(realFile, filepath.Join(sessDir, "auth-refactor.json"))

	src := &SandboxSource{RepoPaths: []string{tmpDir}}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 {
		t.Errorf("len(sessions) = %d, want 1 (symlink should be skipped)", len(sessions))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/scanner/ -run TestSandboxSource -v
```

Expected: FAIL — `SandboxSource` not defined.

- [ ] **Step 4: Implement sandbox scanner**

Create `internal/scanner/sandbox.go`:

```go
package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// sandboxSession mirrors the JSON structure from claude-sandbox state files.
type sandboxSession struct {
	ID           string    `json:"id"`
	Name         string    `json:"name,omitempty"`
	WorktreePath string    `json:"worktree_path"`
	Branch       string    `json:"branch"`
	Status       string    `json:"status"`
	LogPath      string    `json:"log_path"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// SandboxSource discovers sessions from claude-sandbox state directories.
type SandboxSource struct {
	// RepoPaths is a list of repo root directories to scan for .claude-sandbox/sessions/.
	RepoPaths []string
	// LogDir overrides the default log directory (~/.claude/sandbox-sessions/).
	LogDir string
}

func (s *SandboxSource) Scan(_ context.Context) ([]session.Session, error) {
	var sessions []session.Session
	for _, repo := range s.RepoPaths {
		found, err := s.scanRepo(repo)
		if err != nil {
			return nil, fmt.Errorf("scan sandbox repo %s: %w", repo, err)
		}
		sessions = append(sessions, found...)
	}
	return sessions, nil
}

func (s *SandboxSource) scanRepo(repoPath string) ([]session.Session, error) {
	sessDir := filepath.Join(repoPath, ".claude-sandbox", "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		// Skip symlinks (named session aliases).
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		var ss sandboxSession
		if err := json.Unmarshal(data, &ss); err != nil {
			continue
		}

		sess := session.Session{
			ID:        ss.ID,
			Name:      ss.Name,
			Source:    session.SourceSandbox,
			Status:    mapSandboxStatus(ss.Status),
			Dir:       ss.WorktreePath,
			Branch:    ss.Branch,
			StartedAt: ss.StartedAt,
			LogPath:   ss.LogPath,
		}
		if sess.StartedAt.IsZero() {
			sess.StartedAt = ss.CreatedAt
		}

		// Parse log for activity data.
		logPath := s.resolveLogPath(ss)
		if logData, err := os.ReadFile(logPath); err == nil {
			summary := ParseLog(logData)
			sess.LastActivity = summary.LastActivity
			sess.Diagnostics = EvaluateHealth(summary.RecentActivity, time.Now())
			sess.Health = session.WorstHealth(sess.Diagnostics)
		} else {
			sess.Health = session.HealthGreen
		}

		// Try to extract task from PLAN.md.
		if ss.WorktreePath != "" {
			if task := readPlanTitle(ss.WorktreePath); task != "" {
				sess.Task = task
			}
		}

		sessions = append(sessions, sess)
	}

	return sessions, nil
}

func (s *SandboxSource) resolveLogPath(ss sandboxSession) string {
	if s.LogDir != "" {
		return filepath.Join(s.LogDir, ss.ID+".log")
	}
	if ss.LogPath != "" {
		return ss.LogPath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "sandbox-sessions", ss.ID+".log")
}

func readPlanTitle(worktreePath string) string {
	data, err := os.ReadFile(filepath.Join(worktreePath, "PLAN.md"))
	if err != nil {
		return ""
	}
	for _, line := range strings.SplitN(string(data), "\n", 5) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func mapSandboxStatus(status string) session.Status {
	switch status {
	case "speccing":
		return session.StatusSpeccing
	case "ready":
		return session.StatusReady
	case "running":
		return session.StatusRunning
	case "success":
		return session.StatusSuccess
	case "failed":
		return session.StatusFailed
	case "blocked":
		return session.StatusBlocked
	default:
		return session.Status(status)
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/scanner/ -run TestSandboxSource -v
```

Expected: PASS.

Note: `SkipsSymlinks` may need adjustment — `os.ReadDir` `entry.Info()` doesn't report symlinks via `DirEntry.Info()`. The symlink check should use `os.Lstat` instead:

```go
// Replace the symlink check in scanRepo with:
fullPath := filepath.Join(sessDir, entry.Name())
fi, err := os.Lstat(fullPath)
if err != nil {
    continue
}
if fi.Mode()&os.ModeSymlink != 0 {
    continue
}
```

If the test fails on symlink detection, apply this fix then re-run.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner/sandbox.go internal/scanner/sandbox_test.go testdata/sandbox/sessions/
git commit -m "feat(scanner): add sandbox session source with log parsing and PLAN.md extraction"
```

---

### Task 6: Native Session Scanner

**Files:**
- Create: `internal/scanner/native.go`
- Create: `internal/scanner/native_test.go`
- Create: `testdata/native/sessions/12345.json`

- [ ] **Step 1: Create native session fixture**

Create `testdata/native/sessions/12345.json`:

```json
{"pid":12345,"sessionId":"578bd126-4b4b-43ff-aba6-88d872b0cc27","cwd":"/Users/test/dev/myproject","startedAt":1774912561112,"kind":"interactive","entrypoint":"cli"}
```

- [ ] **Step 2: Write failing native scanner test**

Create `internal/scanner/native_test.go`:

```go
package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestNativeSource_Scan(t *testing.T) {
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "native", "sessions", "12345.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "12345.json"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	src := &NativeSource{
		ClaudeDir: tmpDir,
	}

	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	t.Run("discovers session", func(t *testing.T) {
		if len(sessions) != 1 {
			t.Fatalf("len(sessions) = %d, want 1", len(sessions))
		}
	})

	s := sessions[0]

	t.Run("session fields", func(t *testing.T) {
		if s.ID != "578bd126-4b4b-43ff-aba6-88d872b0cc27" {
			t.Errorf("ID = %s, want 578bd126-...", s.ID)
		}
		if s.Source != session.SourceNative {
			t.Errorf("Source = %s, want native", s.Source)
		}
		if s.Dir != "/Users/test/dev/myproject" {
			t.Errorf("Dir = %s, want /Users/test/dev/myproject", s.Dir)
		}
		if s.PID != 12345 {
			t.Errorf("PID = %d, want 12345", s.PID)
		}
	})

	t.Run("status reflects process liveness", func(t *testing.T) {
		// PID 12345 is unlikely to be running in test, so should be idle/dead.
		if s.Status != session.StatusIdle {
			t.Errorf("Status = %s, want idle (stale PID)", s.Status)
		}
	})
}

func TestNativeSource_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	src := &NativeSource{ClaudeDir: tmpDir}
	sessions, err := src.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
go test ./internal/scanner/ -run TestNativeSource -v
```

Expected: FAIL — `NativeSource` not defined.

- [ ] **Step 4: Implement native session scanner**

Create `internal/scanner/native.go`:

```go
package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// nativeSession mirrors the JSON structure from ~/.claude/sessions/<pid>.json.
type nativeSession struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	Cwd        string `json:"cwd"`
	StartedAt  int64  `json:"startedAt"` // Unix milliseconds
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
}

// NativeSource discovers interactive Claude Code sessions.
type NativeSource struct {
	// ClaudeDir overrides the default ~/.claude directory.
	ClaudeDir string
}

func (n *NativeSource) Scan(_ context.Context) ([]session.Session, error) {
	claudeDir := n.ClaudeDir
	if claudeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home dir: %w", err)
		}
		claudeDir = filepath.Join(home, ".claude")
	}

	sessDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read native sessions dir: %w", err)
	}

	var sessions []session.Session
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}

		var ns nativeSession
		if err := json.Unmarshal(data, &ns); err != nil {
			continue
		}

		alive := isProcessAlive(ns.PID)
		status := session.StatusIdle
		if alive {
			status = session.StatusRunning
		}

		sess := session.Session{
			ID:        ns.SessionID,
			Source:    session.SourceNative,
			Status:    status,
			Dir:       ns.Cwd,
			PID:       ns.PID,
			StartedAt: time.UnixMilli(ns.StartedAt),
			Health:    session.HealthGreen,
			Task:      filepath.Base(ns.Cwd),
		}

		if alive {
			// Check last modified time of session file as proxy for activity.
			info, err := entry.Info()
			if err == nil {
				sess.LastActivity = info.ModTime()
			}
		}

		sessions = append(sessions, sess)
	}

	return sessions, nil
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/scanner/ -run TestNativeSource -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner/native.go internal/scanner/native_test.go testdata/native/
git commit -m "feat(scanner): add native Claude session source with process liveness detection"
```

---

### Task 7: Scanner Orchestrator

**Files:**
- Create: `internal/scanner/scanner.go`
- Create: `internal/scanner/scanner_test.go`

- [ ] **Step 1: Write failing orchestrator test**

Create `internal/scanner/scanner_test.go`:

```go
package scanner

import (
	"context"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

type stubSource struct {
	sessions []session.Session
	err      error
}

func (s *stubSource) Scan(_ context.Context) ([]session.Session, error) {
	return s.sessions, s.err
}

func TestScanner_Scan(t *testing.T) {
	sandboxSessions := []session.Session{
		{ID: "sandbox-1", Source: session.SourceSandbox, Status: session.StatusRunning},
	}
	nativeSessions := []session.Session{
		{ID: "native-1", Source: session.SourceNative, Status: session.StatusRunning},
	}

	s := &Scanner{
		Sources: []SessionSource{
			&stubSource{sessions: sandboxSessions},
			&stubSource{sessions: nativeSessions},
		},
	}

	sessions, err := s.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	t.Run("aggregates from all sources", func(t *testing.T) {
		if len(sessions) != 2 {
			t.Fatalf("len(sessions) = %d, want 2", len(sessions))
		}
	})

	t.Run("contains sandbox session", func(t *testing.T) {
		found := false
		for _, s := range sessions {
			if s.ID == "sandbox-1" && s.Source == session.SourceSandbox {
				found = true
			}
		}
		if !found {
			t.Error("sandbox-1 not found")
		}
	})

	t.Run("contains native session", func(t *testing.T) {
		found := false
		for _, s := range sessions {
			if s.ID == "native-1" && s.Source == session.SourceNative {
				found = true
			}
		}
		if !found {
			t.Error("native-1 not found")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/scanner/ -run TestScanner_Scan -v
```

Expected: FAIL — `Scanner`, `SessionSource` not defined.

- [ ] **Step 3: Implement scanner orchestrator**

Create `internal/scanner/scanner.go`:

```go
package scanner

import (
	"context"
	"fmt"
	"sort"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

// SessionSource discovers sessions from a single source.
type SessionSource interface {
	Scan(ctx context.Context) ([]session.Session, error)
}

// Scanner aggregates sessions from multiple sources.
type Scanner struct {
	Sources []SessionSource
}

// Scan collects sessions from all sources, sorted by last activity (most recent first).
func (s *Scanner) Scan(ctx context.Context) ([]session.Session, error) {
	var all []session.Session
	for _, src := range s.Sources {
		sessions, err := src.Scan(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		all = append(all, sessions...)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].LastActivity.After(all[j].LastActivity)
	})

	return all, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/scanner/ -run TestScanner -v
```

Expected: PASS.

- [ ] **Step 5: Run all scanner tests together**

```bash
go test -race ./internal/scanner/ -v
```

Expected: ALL PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/scanner/scanner.go internal/scanner/scanner_test.go
git commit -m "feat(scanner): add orchestrator that aggregates sessions from multiple sources"
```

---

### Task 8: TUI Styles and Keybindings

**Files:**
- Create: `internal/tui/styles.go`
- Create: `internal/tui/keys.go`

- [ ] **Step 1: Define styles**

Create `internal/tui/styles.go`:

```go
package tui

import "charm.land/lipgloss/v2"

// Colors — designed for dark terminals, readable on light.
var (
	colorGreen  = lipgloss.Color("#51bd73")
	colorYellow = lipgloss.Color("#e5c07b")
	colorRed    = lipgloss.Color("#e06c75")
	colorGray   = lipgloss.Color("#5c6370")
	colorDim    = lipgloss.Color("#4b5263")
	colorWhite  = lipgloss.Color("#abb2bf")
	colorAccent = lipgloss.Color("#61afef")
	colorBorder = lipgloss.Color("#3e4452")
)

var (
	// Pane styles.
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	activePaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent)

	paneTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite).
			PaddingLeft(1)

	// Session list styles.
	sessionNameStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorWhite)

	sessionNameSelectedStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(colorAccent)

	sessionMetaStyle = lipgloss.NewStyle().
				Foreground(colorGray).
				PaddingLeft(2)

	// Detail pane styles.
	detailLabelStyle = lipgloss.NewStyle().
				Foreground(colorGray)

	detailValueStyle = lipgloss.NewStyle().
				Foreground(colorWhite)

	detailSectionStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Bold(true)

	// Activity styles.
	activityTimeStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Width(7)

	activityToolStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Width(7)

	activityDetailStyle = lipgloss.NewStyle().
				Foreground(colorGray)

	// Health indicator styles.
	healthGreenStyle  = lipgloss.NewStyle().Foreground(colorGreen)
	healthYellowStyle = lipgloss.NewStyle().Foreground(colorYellow)
	healthRedStyle    = lipgloss.NewStyle().Foreground(colorRed)

	// Diagnostic styles.
	diagnosticWarningStyle  = lipgloss.NewStyle().Foreground(colorYellow)
	diagnosticCriticalStyle = lipgloss.NewStyle().Foreground(colorRed)

	// Progress bar styles.
	progressFilledStyle = lipgloss.NewStyle().Foreground(colorGreen)
	progressEmptyStyle  = lipgloss.NewStyle().Foreground(colorDim)

	// Status bar style.
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			PaddingLeft(1)

	statusBarKeyStyle = lipgloss.NewStyle().
				Foreground(colorGray)
)
```

- [ ] **Step 2: Define keybindings**

Create `internal/tui/keys.go`:

```go
package tui

import tea "charm.land/bubbletea/v2"

type keyAction int

const (
	keyNone keyAction = iota
	keyUp
	keyDown
	keyPeek
	keyNew
	keyStop
	keyClean
	keyLabel
	keyHelp
	keyQuit
)

func parseKey(msg tea.KeyPressMsg) keyAction {
	switch msg.String() {
	case "up", "k":
		return keyUp
	case "down", "j":
		return keyDown
	case "enter":
		return keyPeek
	case "n":
		return keyNew
	case "s":
		return keyStop
	case "c":
		return keyClean
	case "l":
		return keyLabel
	case "?":
		return keyHelp
	case "q", "ctrl+c":
		return keyQuit
	default:
		return keyNone
	}
}
```

- [ ] **Step 3: Verify they compile**

```bash
go get charm.land/bubbletea/v2 charm.land/lipgloss/v2
go build ./internal/tui/...
```

Expected: compiles successfully.

- [ ] **Step 4: Commit**

```bash
git add internal/tui/styles.go internal/tui/keys.go go.mod go.sum
git commit -m "feat(tui): add lipgloss styles and keybinding definitions"
```

---

### Task 9: TUI Session List Component

**Files:**
- Create: `internal/tui/sessions.go`

- [ ] **Step 1: Implement session list component**

Create `internal/tui/sessions.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

type sessionList struct {
	sessions []session.Session
	cursor   int
	width    int
	height   int
}

func newSessionList() sessionList {
	return sessionList{}
}

func (sl *sessionList) SetSize(w, h int) {
	sl.width = w
	sl.height = h
}

func (sl *sessionList) SetSessions(sessions []session.Session) {
	sl.sessions = sessions
	if sl.cursor >= len(sessions) && len(sessions) > 0 {
		sl.cursor = len(sessions) - 1
	}
}

func (sl *sessionList) Up() {
	if sl.cursor > 0 {
		sl.cursor--
	}
}

func (sl *sessionList) Down() {
	if sl.cursor < len(sl.sessions)-1 {
		sl.cursor++
	}
}

func (sl *sessionList) Selected() *session.Session {
	if sl.cursor < len(sl.sessions) {
		return &sl.sessions[sl.cursor]
	}
	return nil
}

func (sl *sessionList) View() string {
	if len(sl.sessions) == 0 {
		return lipgloss.Place(sl.width, sl.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(colorGray).Render("No sessions"))
	}

	var lines []string
	for i, s := range sl.sessions {
		lines = append(lines, sl.renderSession(i, s)...)
	}

	content := strings.Join(lines, "\n")

	// Pad to fill height.
	rendered := strings.Count(content, "\n") + 1
	for rendered < sl.height {
		content += "\n"
		rendered++
	}

	return content
}

func (sl *sessionList) renderSession(idx int, s session.Session) []string {
	selected := idx == sl.cursor

	// Line 1: health dot + name + age
	dot := healthDot(s.Health)
	name := s.Name
	if name == "" {
		name = s.ID
	}
	if selected {
		name = sessionNameSelectedStyle.Render(name)
	} else {
		name = sessionNameStyle.Render(name)
	}
	age := formatAge(s.StartedAt)
	nameWidth := sl.width - 6 // dot + padding + age
	if nameWidth < 10 {
		nameWidth = 10
	}

	line1 := fmt.Sprintf("  %s %s%s",
		dot,
		truncate(lipgloss.NewStyle().Render(name), nameWidth),
		lipgloss.NewStyle().Foreground(colorDim).Render(" "+age),
	)

	// Line 2: source + status
	meta := fmt.Sprintf("%s · %s", s.Source, s.Status)
	line2 := sessionMetaStyle.Render(meta)

	// Line 3: blank spacer
	return []string{line1, line2, ""}
}

func healthDot(h session.Health) string {
	switch h {
	case session.HealthGreen:
		return healthGreenStyle.Render("●")
	case session.HealthYellow:
		return healthYellowStyle.Render("●")
	case session.HealthRed:
		return healthRedStyle.Render("●")
	default:
		return lipgloss.NewStyle().Foreground(colorGray).Render("○")
	}
}

func formatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func truncate(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	// Simple truncation — strip to width - 1 and add ellipsis.
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxWidth-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/tui/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/sessions.go
git commit -m "feat(tui): add session list component with health dots and age display"
```

---

### Task 10: TUI Detail Pane

**Files:**
- Create: `internal/tui/detail.go`

- [ ] **Step 1: Implement detail pane**

Create `internal/tui/detail.go`:

```go
package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

type detailPane struct {
	session  *session.Session
	activity []scanner.ActivityEntry
	width    int
	height   int
	peeking  bool
	// scrollOffset for peek mode.
	scrollOffset int
}

func newDetailPane() detailPane {
	return detailPane{}
}

func (d *detailPane) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *detailPane) SetSession(s *session.Session, activity []scanner.ActivityEntry) {
	d.session = s
	d.activity = activity
	d.scrollOffset = 0
}

func (d *detailPane) TogglePeek() {
	d.peeking = !d.peeking
	d.scrollOffset = 0
}

func (d *detailPane) View() string {
	if d.session == nil {
		return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(colorGray).Render("Select a session"))
	}

	s := d.session
	var sections []string

	// Header info.
	sections = append(sections, d.renderInfo(s))

	// Progress bar for sandbox running sessions.
	if s.Source == session.SourceSandbox && s.Status == session.StatusRunning {
		sections = append(sections, "")
	}

	// Divider.
	divider := detailSectionStyle.Render("── Recent Activity " + strings.Repeat("─", max(0, d.width-22)))
	sections = append(sections, "", divider, "")

	// Activity entries.
	maxEntries := d.height - len(sections) - 4 // Leave room for diagnostics.
	if maxEntries < 1 {
		maxEntries = 1
	}
	start := 0
	if len(d.activity) > maxEntries {
		start = len(d.activity) - maxEntries
	}
	for _, a := range d.activity[start:] {
		timeStr := ""
		if !a.Time.IsZero() {
			timeStr = a.Time.Format("15:04")
		}
		tool := activityToolStyle.Render(a.Tool)
		detail := activityDetailStyle.Render(filepath.Base(a.Detail))
		line := fmt.Sprintf("  %s  %s  %s",
			activityTimeStyle.Render(timeStr),
			tool,
			detail,
		)
		sections = append(sections, line)
	}

	// Diagnostics.
	if len(s.Diagnostics) > 0 {
		sections = append(sections, "")
		for _, diag := range s.Diagnostics {
			icon := "⚠"
			style := diagnosticWarningStyle
			if diag.Severity == session.SeverityCritical {
				icon = "✖"
				style = diagnosticCriticalStyle
			}
			sections = append(sections, "  "+style.Render(icon+" "+diag.Detail))
		}
	}

	content := strings.Join(sections, "\n")

	// Pad to fill height.
	rendered := strings.Count(content, "\n") + 1
	for rendered < d.height {
		content += "\n"
		rendered++
	}

	return content
}

func (d *detailPane) renderInfo(s *session.Session) string {
	var lines []string
	line := func(label, value string) string {
		return fmt.Sprintf("  %s %s",
			detailLabelStyle.Render(label),
			detailValueStyle.Render(value),
		)
	}

	lines = append(lines, line("Source:", string(s.Source)))
	lines = append(lines, line("Dir:   ", s.Dir))
	if s.Branch != "" {
		lines = append(lines, line("Branch:", s.Branch))
	}
	if s.Task != "" {
		lines = append(lines, line("Task:  ", s.Task))
	}

	return strings.Join(lines, "\n")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/tui/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/detail.go
git commit -m "feat(tui): add detail pane with activity log and diagnostic display"
```

---

### Task 11: TUI Status Bar

**Files:**
- Create: `internal/tui/statusbar.go`

- [ ] **Step 1: Implement status bar**

Create `internal/tui/statusbar.go`:

```go
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type statusBar struct {
	width    int
	showHelp bool
}

func newStatusBar() statusBar {
	return statusBar{}
}

func (sb *statusBar) SetWidth(w int) {
	sb.width = w
}

func (sb *statusBar) ToggleHelp() {
	sb.showHelp = !sb.showHelp
}

func (sb *statusBar) View() string {
	if sb.showHelp {
		return sb.helpView()
	}

	bindings := []struct{ key, desc string }{
		{"↑↓", "navigate"},
		{"enter", "peek"},
		{"n", "new"},
		{"s", "stop"},
		{"c", "clean"},
		{"l", "label"},
		{"?", "help"},
		{"q", "quit"},
	}

	var parts []string
	for _, b := range bindings {
		parts = append(parts,
			statusBarKeyStyle.Render(b.key)+" "+statusBarStyle.Render(b.desc),
		)
	}

	line := " " + strings.Join(parts, statusBarStyle.Render(" · "))
	return lipgloss.NewStyle().Width(sb.width).Render(line)
}

func (sb *statusBar) helpView() string {
	help := []string{
		"  " + statusBarKeyStyle.Render("↑/↓ or j/k") + "  Navigate sessions",
		"  " + statusBarKeyStyle.Render("enter") + "      Toggle peek (scrollable log)",
		"  " + statusBarKeyStyle.Render("n") + "          New session",
		"  " + statusBarKeyStyle.Render("s") + "          Stop selected session",
		"  " + statusBarKeyStyle.Render("c") + "          Clean completed/failed",
		"  " + statusBarKeyStyle.Render("l") + "          Label selected session",
		"  " + statusBarKeyStyle.Render("?") + "          Toggle this help",
		"  " + statusBarKeyStyle.Render("q") + "          Quit (sessions keep running)",
	}
	return strings.Join(help, "\n")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/tui/...
```

Expected: success.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/statusbar.go
git commit -m "feat(tui): add status bar with keybinding hints and help overlay"
```

---

### Task 12: TUI App Model

**Files:**
- Create: `internal/tui/app.go`
- Create: `internal/tui/app_test.go`

- [ ] **Step 1: Write failing app test**

Create `internal/tui/app_test.go`:

```go
package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestApp_KeyNavigation(t *testing.T) {
	app := NewApp(nil)
	app.sessions.sessions = []session.Session{
		{ID: "s1", Name: "first", Source: session.SourceSandbox, Health: session.HealthGreen},
		{ID: "s2", Name: "second", Source: session.SourceNative, Health: session.HealthYellow},
	}

	t.Run("initial cursor at 0", func(t *testing.T) {
		if app.sessions.cursor != 0 {
			t.Errorf("cursor = %d, want 0", app.sessions.cursor)
		}
	})

	t.Run("down moves cursor", func(t *testing.T) {
		msg := tea.KeyPressMsg{Code: 'j'}
		updated, _ := app.Update(msg)
		app = updated.(*App)
		if app.sessions.cursor != 1 {
			t.Errorf("cursor = %d, want 1", app.sessions.cursor)
		}
	})

	t.Run("up moves cursor back", func(t *testing.T) {
		msg := tea.KeyPressMsg{Code: 'k'}
		updated, _ := app.Update(msg)
		app = updated.(*App)
		if app.sessions.cursor != 0 {
			t.Errorf("cursor = %d, want 0", app.sessions.cursor)
		}
	})

	t.Run("question mark toggles help", func(t *testing.T) {
		msg := tea.KeyPressMsg{Code: '?'}
		updated, _ := app.Update(msg)
		app = updated.(*App)
		if !app.statusbar.showHelp {
			t.Error("expected showHelp = true")
		}
	})
}

func TestApp_TickUpdatesSessions(t *testing.T) {
	app := NewApp(nil)

	// Simulate a tick message with session data.
	sessions := []session.Session{
		{ID: "s1", Name: "test", LastActivity: time.Now()},
	}
	msg := tickMsg{sessions: sessions}
	updated, _ := app.Update(msg)
	app = updated.(*App)

	if len(app.sessions.sessions) != 1 {
		t.Errorf("sessions count = %d, want 1", len(app.sessions.sessions))
	}
	if app.sessions.sessions[0].ID != "s1" {
		t.Errorf("session ID = %s, want s1", app.sessions.sessions[0].ID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/tui/ -run TestApp -v
```

Expected: FAIL — `NewApp`, `App`, `tickMsg` not defined.

- [ ] **Step 3: Implement the app model**

Create `internal/tui/app.go`:

```go
package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
)

const tickInterval = 3 * time.Second

// tickMsg carries refreshed session data from the scanner.
type tickMsg struct {
	sessions   []session.Session
	activities map[string][]scanner.ActivityEntry
	err        error
}

// App is the root Bubbletea model.
type App struct {
	scanner   *scanner.Scanner
	sessions  sessionList
	detail    detailPane
	statusbar statusBar
	width     int
	height    int
	// activities caches parsed log activity per session ID.
	activities map[string][]scanner.ActivityEntry
}

// NewApp creates a new TUI application.
func NewApp(sc *scanner.Scanner) *App {
	return &App{
		scanner:    sc,
		sessions:   newSessionList(),
		detail:     newDetailPane(),
		statusbar:  newStatusBar(),
		activities: make(map[string][]scanner.ActivityEntry),
	}
}

func (a *App) Init() tea.Cmd {
	return a.tick()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.updateLayout()
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case tickMsg:
		if msg.err == nil {
			a.sessions.SetSessions(msg.sessions)
			a.activities = msg.activities
			a.updateDetail()
		}
		return a, a.tick()
	}

	return a, nil
}

func (a *App) View() tea.View {
	if a.width == 0 || a.height == 0 {
		return tea.NewView("Loading...")
	}

	// Calculate pane widths.
	listWidth := a.width * 30 / 100
	if listWidth < 20 {
		listWidth = 20
	}
	detailWidth := a.width - listWidth

	contentHeight := a.height - 2 // Status bar + border.

	// Render panes.
	a.sessions.SetSize(listWidth-2, contentHeight-2)   // Minus border.
	a.detail.SetSize(detailWidth-2, contentHeight-2)
	a.statusbar.SetWidth(a.width)

	leftContent := a.sessions.View()
	rightContent := a.detail.View()

	sel := a.sessions.Selected()
	leftTitle := " Sessions "
	rightTitle := " Detail "
	if sel != nil {
		name := sel.Name
		if name == "" {
			name = sel.ID
		}
		rightTitle = " " + name + " "
	}

	leftPane := paneStyle.
		Width(listWidth - 2).
		Height(contentHeight - 2).
		Render(leftContent)
	leftPane = paneTitleStyle.Render(leftTitle) + "\n" + leftPane

	rightPane := paneStyle.
		Width(detailWidth - 2).
		Height(contentHeight - 2).
		Render(rightContent)
	rightPane = paneTitleStyle.Render(rightTitle) + "\n" + rightPane

	body := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
	statusLine := a.statusbar.View()

	view := lipgloss.JoinVertical(lipgloss.Left, body, statusLine)

	v := tea.NewView(view)
	v.AltScreen = true
	return v
}

func (a *App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	action := parseKey(msg)
	switch action {
	case keyQuit:
		return a, tea.Quit
	case keyUp:
		a.sessions.Up()
		a.updateDetail()
	case keyDown:
		a.sessions.Down()
		a.updateDetail()
	case keyPeek:
		a.detail.TogglePeek()
	case keyHelp:
		a.statusbar.ToggleHelp()
	}
	return a, nil
}

func (a *App) updateLayout() {
	// Recalculate sizes on window resize.
	listWidth := a.width * 30 / 100
	detailWidth := a.width - listWidth
	contentHeight := a.height - 2
	a.sessions.SetSize(listWidth, contentHeight)
	a.detail.SetSize(detailWidth, contentHeight)
	a.statusbar.SetWidth(a.width)
}

func (a *App) updateDetail() {
	sel := a.sessions.Selected()
	if sel == nil {
		a.detail.SetSession(nil, nil)
		return
	}
	activity := a.activities[sel.ID]
	a.detail.SetSession(sel, activity)
}

func (a *App) tick() tea.Cmd {
	return tea.Tick(tickInterval, func(_ time.Time) tea.Msg {
		if a.scanner == nil {
			return tickMsg{}
		}
		sessions, err := a.scanner.Scan(context.Background())
		if err != nil {
			return tickMsg{err: err}
		}

		activities := make(map[string][]scanner.ActivityEntry)
		for _, s := range sessions {
			if s.LogPath != "" {
				// Read and parse log for activity entries.
				// This is done in the tick command (not Update) to avoid blocking the UI.
				if data, err := readLogTail(s.LogPath); err == nil {
					summary := scanner.ParseLog(data)
					activities[s.ID] = summary.RecentActivity
				}
			}
		}

		return tickMsg{sessions: sessions, activities: activities}
	})
}

func readLogTail(path string) ([]byte, error) {
	// Read up to the last 64KB of the log file for performance.
	const maxBytes = 64 * 1024
	f, err := openFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size <= maxBytes {
		return readAll(f)
	}

	buf := make([]byte, maxBytes)
	_, err = f.ReadAt(buf, size-maxBytes)
	return buf, err
}
```

Wait — `openFile` and `readAll` aren't defined. Use `os` directly:

Replace the `readLogTail` function with:

```go
import "os"

func readLogTail(path string) ([]byte, error) {
	const maxBytes = 64 * 1024
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size := info.Size()
	if size <= maxBytes {
		return os.ReadFile(path)
	}

	buf := make([]byte, maxBytes)
	_, err = f.ReadAt(buf, size-maxBytes)
	return buf, err
}
```

Ensure the `os` import is in the import block at the top of `app.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui/ -run TestApp -v
```

Expected: PASS. If `tea.KeyPressMsg{Code: 'j'}` doesn't match the key properly in Bubbletea v2, adjust the test to use the correct struct fields per the v2 API. Check the actual `tea.KeyPressMsg` struct definition and adapt.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go
git commit -m "feat(tui): add root app model with tick-based scanning and split-pane layout"
```

---

### Task 13: CLI Subcommands

**Files:**
- Create: `cmd/cs/root.go`
- Create: `cmd/cs/ls.go`
- Create: `cmd/cs/peek.go`
- Create: `cmd/cs/stop.go`
- Create: `cmd/cs/label.go`
- Create: `cmd/cs/clean.go`
- Create: `cmd/cs/new.go`
- Modify: `cmd/cs/main.go`

- [ ] **Step 1: Create root command with TUI as default**

Create `cmd/cs/root.go`:

```go
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/tui"
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cs",
		Short: "Manage multiple Claude Code sessions",
		Long:  "TUI + CLI for managing interactive and autonomous Claude Code sessions.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc := buildScanner()
			app := tui.NewApp(sc)
			p := tea.NewProgram(app)
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("run TUI: %w", err)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.AddCommand(newLsCommand())
	cmd.AddCommand(newPeekCommand())
	cmd.AddCommand(newStopCommand())
	cmd.AddCommand(newLabelCommand())
	cmd.AddCommand(newCleanCommand())
	cmd.AddCommand(newNewCommand())
	cmd.AddCommand(newVersionCommand())

	return cmd
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Println("cs", version)
		},
	}
}

func buildScanner() *scanner.Scanner {
	home, _ := os.UserHomeDir()

	// Discover repos with .claude-sandbox by checking common locations.
	// For now, scan cwd and any repos with active sandbox sessions.
	cwd, _ := os.Getwd()

	return &scanner.Scanner{
		Sources: []scanner.SessionSource{
			&scanner.SandboxSource{
				RepoPaths: []string{cwd},
			},
			&scanner.NativeSource{
				ClaudeDir: home + "/.claude",
			},
		},
	}
}
```

- [ ] **Step 2: Create ls subcommand**

Create `cmd/cs/ls.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newLsCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List all sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			if jsonOutput {
				return printJSON(cmd, sessions)
			}
			return printTable(cmd, sessions)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func printJSON(cmd *cobra.Command, sessions []session.Session) error {
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	cmd.Println(string(data))
	return nil
}

func printTable(cmd *cobra.Command, sessions []session.Session) error {
	if len(sessions) == 0 {
		cmd.Println("No sessions found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HEALTH\tNAME\tSOURCE\tSTATUS\tDIR")
	for _, s := range sessions {
		name := s.Name
		if name == "" {
			name = s.ID
		}
		dot := healthSymbol(s.Health)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", dot, name, s.Source, s.Status, s.Dir)
	}
	return w.Flush()
}

func healthSymbol(h session.Health) string {
	switch h {
	case session.HealthGreen:
		return "●"
	case session.HealthYellow:
		return "◉"
	case session.HealthRed:
		return "✖"
	default:
		return "○"
	}
}
```

- [ ] **Step 3: Create peek subcommand**

Create `cmd/cs/peek.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/dakaneye/claude-session-manager/internal/scanner"
	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newPeekCommand() *cobra.Command {
	var lines int

	cmd := &cobra.Command{
		Use:   "peek [session]",
		Short: "Tail session log output",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			sess := findSession(sessions, args[0])
			if sess == nil {
				return fmt.Errorf("session not found: %s", args[0])
			}

			if sess.LogPath == "" {
				return fmt.Errorf("no log path for session %s (source: %s)", sess.ID, sess.Source)
			}

			data, err := os.ReadFile(sess.LogPath)
			if err != nil {
				return fmt.Errorf("read log: %w", err)
			}

			summary := scanner.ParseLog(data)
			maxEntries := lines
			start := 0
			if len(summary.RecentActivity) > maxEntries {
				start = len(summary.RecentActivity) - maxEntries
			}
			for _, a := range summary.RecentActivity[start:] {
				ts := ""
				if !a.Time.IsZero() {
					ts = a.Time.Format(time.TimeOnly)
				}
				errMark := ""
				if a.IsError {
					errMark = " [ERROR]"
				}
				cmd.Printf("%s  %-6s  %s%s\n", ts, a.Tool, a.Detail, errMark)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&lines, "lines", "n", 20, "Number of recent activities to show")
	return cmd
}

func findSession(sessions []session.Session, query string) *session.Session {
	for i, s := range sessions {
		if s.ID == query || s.Name == query {
			return &sessions[i]
		}
	}
	return nil
}
```

- [ ] **Step 4: Create stop subcommand**

Create `cmd/cs/stop.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"syscall"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [session]",
		Short: "Stop a running session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			sess := findSession(sessions, args[0])
			if sess == nil {
				return fmt.Errorf("session not found: %s", args[0])
			}

			if sess.Source == session.SourceNative && sess.PID > 0 {
				proc, err := os.FindProcess(sess.PID)
				if err != nil {
					return fmt.Errorf("find process %d: %w", sess.PID, err)
				}
				if err := proc.Signal(syscall.SIGTERM); err != nil {
					return fmt.Errorf("signal process %d: %w", sess.PID, err)
				}
				cmd.Printf("Sent SIGTERM to PID %d (%s)\n", sess.PID, sess.ID)
				return nil
			}

			if sess.Source == session.SourceSandbox {
				cmd.PrintErrln("Sandbox sessions should be stopped with: claude-sandbox stop " + sess.ID)
				return nil
			}

			return fmt.Errorf("cannot stop session %s (source: %s)", sess.ID, sess.Source)
		},
	}
}
```

- [ ] **Step 5: Create label subcommand**

Create `cmd/cs/label.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newLabelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "label [session] [description]",
		Short: "Set a task label on a session",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			sess := findSession(sessions, args[0])
			if sess == nil {
				return fmt.Errorf("session not found: %s", args[0])
			}

			// Store labels in ~/.claude/session-labels/<session-id>.json.
			home, _ := os.UserHomeDir()
			labelDir := filepath.Join(home, ".claude", "session-labels")
			if err := os.MkdirAll(labelDir, 0o755); err != nil {
				return fmt.Errorf("create label dir: %w", err)
			}

			label := map[string]string{
				"session_id": sess.ID,
				"label":      args[1],
			}
			data, _ := json.MarshalIndent(label, "", "  ")
			labelPath := filepath.Join(labelDir, sess.ID+".json")
			if err := os.WriteFile(labelPath, data, 0o644); err != nil {
				return fmt.Errorf("write label: %w", err)
			}

			cmd.Printf("Labeled %s: %s\n", sess.ID, args[1])
			return nil
		},
	}
}

// readLabel reads a session label from the label store.
func readLabel(sessionID string) string {
	home, _ := os.UserHomeDir()
	labelPath := filepath.Join(home, ".claude", "session-labels", sessionID+".json")
	data, err := os.ReadFile(labelPath)
	if err != nil {
		return ""
	}
	var label map[string]string
	if err := json.Unmarshal(data, &label); err != nil {
		return ""
	}
	return label["label"]
}
```

- [ ] **Step 6: Create clean subcommand**

Create `cmd/cs/clean.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/dakaneye/claude-session-manager/internal/session"
	"github.com/spf13/cobra"
)

func newCleanCommand() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove completed or failed sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc := buildScanner()
			sessions, err := sc.Scan(context.Background())
			if err != nil {
				return fmt.Errorf("scan sessions: %w", err)
			}

			cleaned := 0
			for _, s := range sessions {
				if !all && s.Status != session.StatusSuccess && s.Status != session.StatusFailed {
					continue
				}
				if s.Source == session.SourceSandbox {
					cmd.Printf("Clean sandbox session %s with: claude-sandbox clean --session %s\n", s.ID, s.ID)
				} else if s.Source == session.SourceNative {
					cmd.Printf("Native session %s (pid %d) - remove stale session file manually if process is dead\n", s.ID, s.PID)
				}
				cleaned++
			}

			if cleaned == 0 {
				cmd.Println("Nothing to clean.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Clean all sessions, not just completed/failed")
	return cmd
}
```

- [ ] **Step 7: Create new subcommand**

Create `cmd/cs/new.go`:

```go
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newNewCommand() *cobra.Command {
	var (
		sandbox     bool
		interactive bool
		dir         string
		name        string
	)

	cmd := &cobra.Command{
		Use:   "new",
		Short: "Create a new Claude session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dir == "" {
				var err error
				dir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("get working directory: %w", err)
				}
			}

			if sandbox {
				return launchSandboxSession(cmd, dir, name)
			}
			return launchInteractiveSession(cmd, dir)
		},
	}

	cmd.Flags().BoolVar(&sandbox, "sandbox", false, "Create a sandbox (autonomous) session")
	cmd.Flags().BoolVar(&interactive, "interactive", true, "Create an interactive session")
	cmd.Flags().StringVar(&dir, "dir", "", "Working directory (default: cwd)")
	cmd.Flags().StringVar(&name, "name", "", "Session name (sandbox only)")
	return cmd
}

func launchSandboxSession(cmd *cobra.Command, dir, name string) error {
	args := []string{"spec"}
	if name != "" {
		args = append(args, "--name", name)
	}
	args = append(args, "--dir", dir)

	c := exec.Command("claude-sandbox", args...)
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("launch claude-sandbox spec: %w", err)
	}
	return nil
}

func launchInteractiveSession(cmd *cobra.Command, dir string) error {
	c := exec.Command("claude")
	c.Dir = dir
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("launch claude: %w", err)
	}
	return nil
}
```

- [ ] **Step 8: Update main.go to use Cobra**

Replace `cmd/cs/main.go`:

```go
package main

import "os"

var version = "dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 9: Add cobra dependency and verify it compiles**

```bash
go get github.com/spf13/cobra
go build ./cmd/cs/
```

Expected: compiles successfully, produces `cs` binary.

- [ ] **Step 10: Verify cs version works**

```bash
./cs version
```

Expected: `cs dev`

- [ ] **Step 11: Verify cs ls works (empty results expected)**

```bash
./cs ls
```

Expected: `No sessions found.` or a list of native sessions.

- [ ] **Step 12: Commit**

```bash
git add cmd/cs/ go.mod go.sum
git commit -m "feat(cli): add all subcommands (ls, peek, stop, label, clean, new) and TUI entry point"
```

---

### Task 14: Integration Test

**Files:**
- Create: `internal/scanner/integration_test.go`

- [ ] **Step 1: Write integration test**

Create `internal/scanner/integration_test.go`:

```go
package scanner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dakaneye/claude-session-manager/internal/session"
)

func TestIntegration_FullScan(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up sandbox session.
	sandboxDir := filepath.Join(tmpDir, "repo")
	sessDir := filepath.Join(sandboxDir, ".claude-sandbox", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sandboxJSON := map[string]any{
		"id":            "2026-03-27-test01",
		"name":          "integration-test",
		"worktree_path": sandboxDir,
		"branch":        "sandbox/2026-03-27-test01",
		"status":        "running",
		"log_path":      "",
		"created_at":    "2026-03-27T14:00:00Z",
		"started_at":    "2026-03-27T14:30:00Z",
	}
	data, _ := json.MarshalIndent(sandboxJSON, "", "  ")
	os.WriteFile(filepath.Join(sessDir, "2026-03-27-test01.json"), data, 0o644)

	// Set up log for sandbox session.
	logDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logDir, 0o755)
	logFixture, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "sandbox", "logs", "2026-03-27-abc123.log"))
	os.WriteFile(filepath.Join(logDir, "2026-03-27-test01.log"), logFixture, 0o644)

	// Set up PLAN.md in worktree.
	os.WriteFile(filepath.Join(sandboxDir, "PLAN.md"), []byte("# Refactor auth middleware\n\nSome plan."), 0o644)

	// Set up native session.
	claudeDir := filepath.Join(tmpDir, "claude")
	nativeSessDir := filepath.Join(claudeDir, "sessions")
	os.MkdirAll(nativeSessDir, 0o755)
	nativeJSON := map[string]any{
		"pid":        99999999, // Unlikely to be alive.
		"sessionId":  "native-test-uuid",
		"cwd":        "/tmp/test-project",
		"startedAt":  1774912561112,
		"kind":       "interactive",
		"entrypoint": "cli",
	}
	nativeData, _ := json.MarshalIndent(nativeJSON, "", "  ")
	os.WriteFile(filepath.Join(nativeSessDir, "99999999.json"), nativeData, 0o644)

	// Build scanner with both sources.
	sc := &Scanner{
		Sources: []SessionSource{
			&SandboxSource{
				RepoPaths: []string{sandboxDir},
				LogDir:    logDir,
			},
			&NativeSource{
				ClaudeDir: claudeDir,
			},
		},
	}

	sessions, err := sc.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	t.Run("finds both session types", func(t *testing.T) {
		if len(sessions) != 2 {
			t.Fatalf("len(sessions) = %d, want 2", len(sessions))
		}
	})

	// Find each session.
	var sandbox, native *session.Session
	for i := range sessions {
		switch sessions[i].Source {
		case session.SourceSandbox:
			sandbox = &sessions[i]
		case session.SourceNative:
			native = &sessions[i]
		}
	}

	t.Run("sandbox session fully populated", func(t *testing.T) {
		if sandbox == nil {
			t.Fatal("sandbox session not found")
		}
		if sandbox.ID != "2026-03-27-test01" {
			t.Errorf("ID = %s", sandbox.ID)
		}
		if sandbox.Name != "integration-test" {
			t.Errorf("Name = %s", sandbox.Name)
		}
		if sandbox.Status != session.StatusRunning {
			t.Errorf("Status = %s", sandbox.Status)
		}
		if sandbox.Task != "Refactor auth middleware" {
			t.Errorf("Task = %q, want 'Refactor auth middleware'", sandbox.Task)
		}
		if sandbox.LastActivity.IsZero() {
			t.Error("LastActivity is zero, expected parsed from log")
		}
	})

	t.Run("native session populated", func(t *testing.T) {
		if native == nil {
			t.Fatal("native session not found")
		}
		if native.ID != "native-test-uuid" {
			t.Errorf("ID = %s", native.ID)
		}
		if native.Status != session.StatusIdle {
			t.Errorf("Status = %s, want idle (stale PID)", native.Status)
		}
		if native.Dir != "/tmp/test-project" {
			t.Errorf("Dir = %s", native.Dir)
		}
	})

	t.Run("sandbox session has health diagnostics from log", func(t *testing.T) {
		if sandbox == nil {
			t.Skip("no sandbox session")
		}
		// The fixture log has 2 consecutive test failures then a pass.
		// The last 3 entries end with a pass, so test-loop shouldn't fire.
		// But there are 2 edits to the same file, which is under threshold.
		// Health should be green for this fixture.
		if sandbox.Health != session.HealthGreen {
			t.Errorf("Health = %s, want green (fixture is healthy)", sandbox.Health)
		}
	})
}
```

- [ ] **Step 2: Run the integration test**

```bash
go test -race ./internal/scanner/ -run TestIntegration -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/scanner/integration_test.go
git commit -m "test: add integration test covering full scan across sandbox and native sources"
```

---

### Task 15: Makefile Verify Target and Final Wiring

**Files:**
- Modify: `Makefile` (already created in Task 1)

- [ ] **Step 1: Run the full verify chain**

```bash
make verify
```

Expected: build, vet, lint, test (with -race), and tidy all pass. If any step fails, fix the issue before proceeding.

- [ ] **Step 2: Fix any compilation or lint issues**

Common issues to check:
- Unused imports (remove them)
- Missing error checks flagged by errcheck (handle or explicitly ignore with comment)
- Any `go vet` warnings

- [ ] **Step 3: Run verify again to confirm clean**

```bash
make verify
```

Expected: `All checks passed`

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "fix: resolve lint and vet issues for clean verify"
```

---

### Task 16: Manual Smoke Test

This is NOT an automated test — it's a manual verification that the tool actually works end-to-end.

- [ ] **Step 1: Build and install**

```bash
make install
```

- [ ] **Step 2: Verify cs ls shows real sessions**

```bash
cs ls
```

Expected: shows your currently running native Claude sessions (you should see at least this session). If no sessions appear, debug the native scanner against `~/.claude/sessions/`.

- [ ] **Step 3: Verify cs ls --json outputs valid JSON**

```bash
cs ls --json | python3 -m json.tool
```

Expected: valid JSON array of session objects.

- [ ] **Step 4: Launch the TUI**

```bash
cs
```

Expected: TUI launches in alt screen, shows session list on left, detail on right, status bar at bottom. Navigate with j/k, press ? for help, press q to quit. Sessions should be visible.

- [ ] **Step 5: If anything doesn't work, fix it**

The bar: if it compiles but doesn't actually show sessions or the TUI is broken, that's a failure. Fix before considering this task complete.

- [ ] **Step 6: Final commit if fixes were needed**

```bash
git add -A
git commit -m "fix: resolve issues found during manual smoke test"
```
