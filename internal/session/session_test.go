package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDisplayName(t *testing.T) {
	tests := []struct {
		name string
		sess Session
		want string
	}{
		{"returns name when set", Session{ID: "abc", Name: "my-session"}, "my-session"},
		{"falls back to ID", Session{ID: "abc-123"}, "abc-123"},
		{"empty name uses ID", Session{ID: "id", Name: ""}, "id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sess.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWorstHealth(t *testing.T) {
	tests := []struct {
		name        string
		diagnostics []Diagnostic
		want        Health
	}{
		{"no diagnostics", nil, HealthGreen},
		{"empty slice", []Diagnostic{}, HealthGreen},
		{"warning only", []Diagnostic{
			{Signal: "test", Severity: SeverityWarning},
		}, HealthYellow},
		{"critical returns red", []Diagnostic{
			{Signal: "test", Severity: SeverityCritical},
		}, HealthRed},
		{"critical overrides warning", []Diagnostic{
			{Signal: "warn", Severity: SeverityWarning},
			{Signal: "crit", Severity: SeverityCritical},
		}, HealthRed},
		{"multiple warnings stay yellow", []Diagnostic{
			{Signal: "w1", Severity: SeverityWarning},
			{Signal: "w2", Severity: SeverityWarning},
		}, HealthYellow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WorstHealth(tt.diagnostics); got != tt.want {
				t.Errorf("WorstHealth() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsProcessAlive(t *testing.T) {
	t.Run("current process is alive", func(t *testing.T) {
		if !IsProcessAlive(os.Getpid()) {
			t.Error("current process should be alive")
		}
	})

	t.Run("zero PID returns false", func(t *testing.T) {
		if IsProcessAlive(0) {
			t.Error("PID 0 should not be alive")
		}
	})

	t.Run("negative PID returns false", func(t *testing.T) {
		if IsProcessAlive(-1) {
			t.Error("negative PID should not be alive")
		}
	})

	t.Run("nonexistent PID returns false", func(t *testing.T) {
		if IsProcessAlive(99999999) {
			t.Error("PID 99999999 should not be alive")
		}
	})
}

func TestDefaultStateDir(t *testing.T) {
	dir, err := DefaultStateDir()
	if err != nil {
		t.Fatalf("DefaultStateDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DefaultStateDir returned relative path: %q", dir)
	}
	if filepath.Base(dir) != "cs-sessions" {
		t.Errorf("DefaultStateDir base = %q, want cs-sessions", filepath.Base(dir))
	}
}

func TestManagedMetaPath(t *testing.T) {
	path, err := ManagedMetaPath("test-id")
	if err != nil {
		t.Fatalf("ManagedMetaPath: %v", err)
	}
	if filepath.Base(path) != "test-id.json" {
		t.Errorf("ManagedMetaPath base = %q, want test-id.json", filepath.Base(path))
	}
}

func TestWriteAndReadLabel(t *testing.T) {
	// Override HOME to use temp dir for label storage.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	err := WriteLabel("sess-1", "bug fix")
	if err != nil {
		t.Fatalf("WriteLabel: %v", err)
	}

	got := ReadLabel("sess-1")
	if got != "bug fix" {
		t.Errorf("ReadLabel = %q, want %q", got, "bug fix")
	}

	// Nonexistent label returns empty.
	if got := ReadLabel("nonexistent"); got != "" {
		t.Errorf("ReadLabel(nonexistent) = %q, want empty", got)
	}
}

func TestManagedMetaJSON(t *testing.T) {
	meta := ManagedMeta{
		ID:      "test-1",
		PID:     12345,
		Dir:     "/tmp/project",
		Source:  SourceNative,
		Managed: true,
	}

	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ManagedMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Source != SourceNative {
		t.Errorf("Source = %q, want %q", decoded.Source, SourceNative)
	}
	if decoded.ID != "test-1" {
		t.Errorf("ID = %q, want test-1", decoded.ID)
	}
}
