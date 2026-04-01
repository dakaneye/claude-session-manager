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

func TestDeduplicateByPID(t *testing.T) {
	sessions := []session.Session{
		{ID: "native-1", PID: 1234, Managed: false, Source: session.SourceNative},
		{ID: "managed-1", PID: 1234, Managed: true, Source: session.SourceNative},
		{ID: "sandbox-1", PID: 0, Managed: false, Source: session.SourceSandbox},
	}

	result := deduplicateByPID(sessions)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}

	for _, s := range result {
		if s.PID == 1234 && !s.Managed {
			t.Error("PID 1234 should be managed session, got discovered")
		}
	}

	found := false
	for _, s := range result {
		if s.ID == "sandbox-1" {
			found = true
		}
	}
	if !found {
		t.Error("sandbox session should be preserved")
	}
}
