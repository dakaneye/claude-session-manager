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
	StatusRunning   Status = "running"
	StatusIdle      Status = "idle"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusBlocked   Status = "blocked"
	StatusSpeccing  Status = "speccing"
	StatusReady     Status = "ready"
	StatusStopped   Status = "stopped"
	StatusExecuting Status = "executing"
	StatusShipping  Status = "shipping"
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
	PID          int          `json:"pid,omitempty"`
	LogPath      string       `json:"log_path,omitempty"`
	Managed      bool         `json:"managed,omitempty"`
}

// DisplayName returns the session's name, falling back to its ID.
func (s Session) DisplayName() string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
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
