# Claude Session Manager (`cs`) — Design Spec

## Problem

Managing multiple Claude Code sessions is painful. You lose track of what each session is doing, which are stuck, and switching is manual. Existing tools (amux, claude-squad, ccmanager) all require tmux, have fragile ANSI parsing, or lack visibility into session state and health.

No tool combines: no-tmux dependency + session state visibility + peek into active sessions + stuck/anomaly detection.

## Solution

A Go TUI + CLI (`cs`) for managing multiple Claude Code sessions — both interactive (native Claude) and autonomous (claude-sandbox). A scanner layer discovers sessions from both sources, reads logs and state files, and runs health heuristics to surface problems. The TUI shows a split-pane dashboard with green/yellow/red health indicators. CLI subcommands work without the TUI for scripting.

## Architecture

### Two-Layer Design

```
+---------------------------------------------+
|  TUI Layer (Bubbletea v2)                   |
|  Model -> Update -> View                    |
|  Components: session list, detail viewport  |
+---------------------------------------------+
|  Scanner Layer (pure Go, no TUI dependency) |
|  Reads session state, logs, health signals  |
|  Returns typed structs, no side effects     |
+-----------+--------------+------------------+
            |              |
      +-----v-----+  +----v------------------+
      | Claude     |  | claude-sandbox        |
      | native     |  | .claude-sandbox/      |
      | sessions   |  | sessions/*.json       |
      | ~/.claude  |  | stream-JSON logs      |
      +-----------+   +-----------------------+
```

**Scanner layer** (`internal/scanner`): Discovers sessions from both sources, reads state files and log tails, runs health heuristics, returns `[]Session`. No TUI imports — testable in isolation, reusable by CLI subcommands and future dashboard/API.

**TUI layer** (`internal/tui`): Bubbletea v2 Elm architecture. Calls scanner on a `tea.Tick` (every 3 seconds). Renders session list + detail pane. Handles keyboard navigation and actions.

**Entry point**: Cobra command (`cmd/cs/main.go`). `cs` launches TUI by default. Subcommands work standalone.

## Session Model

```go
type Session struct {
    ID           string
    Name         string
    Source       Source       // Native | Sandbox
    Status       Status       // Running | Idle | Success | Failed | Blocked
    Health       Health       // Green | Yellow | Red
    Dir          string
    Branch       string
    StartedAt    time.Time
    LastActivity time.Time
    Task         string       // PLAN.md title or manual label
    Diagnostics  []Diagnostic
}

type Diagnostic struct {
    Signal   string   // e.g. "repeated-edit", "test-loop", "high-context"
    Severity Severity // Warning | Critical
    Detail   string   // "auth.go edited 7 times in 4 minutes"
}
```

### Session Sources

**Sandbox sessions**: Read `.claude-sandbox/sessions/*.json` for state. Tail log at `~/.claude/sandbox-sessions/<id>.log` (stream-JSON format) for tool use, timestamps, errors.

**Native interactive sessions**: Read `~/.claude/sessions/*.json` for PID, cwd, sessionId. Check process liveness via `os.FindProcess` + signal 0. Read session conversation artifacts where accessible for health heuristics. Fall back to alive/dead + last-modified timestamps.

### Task Detection

- Sandbox sessions: parse first heading from `PLAN.md` in the worktree
- Interactive sessions: working directory name as default label
- Manual override via `cs label <session> "description"`

## Health Heuristics

Run on each scanner tick (3 seconds). Log-analysis based (reactive), designed to evolve to hook-driven (proactive) in v2.

| Signal | Source | Threshold | Severity |
|--------|--------|-----------|----------|
| Same file edited N+ times | stream-JSON log | 5 edits/5 min | Yellow; 8+ Red |
| Test failing in loop | repeated test exits non-zero | 3 consecutive | Red |
| No tool use for N minutes | log timestamp gap | 5 min Yellow, 10 min Red |
| Context window high | gsd-context-monitor metrics | 65% Yellow, 75% Red |
| Repeated identical command | log: same bash command | 3 in a row | Yellow |
| Long runtime without commits | git log in session dir | 30 min no commit | Yellow |

Diagnostics surface inline in the detail pane, not as popups or modals.

## TUI Layout

Left/right split — session list (30%) + detail pane (70%). Responsive to terminal width. Bottom status bar with keybinding hints.

```
 +- Sessions ---------------------++- auth-refactor --------------------------+
 |                                ||                                          |
 |  * auth-refactor           18m ||  sandbox . ~/dev/myproject               |
 |    sandbox . Running           ||  sandbox/2026-03-30-a3f2b1              |
 |                                ||  Refactor auth middleware for compliance |
 |  o api-tests              45s  ||                                          |
 |    sandbox . Running           ||  Progress ========..............  43%    |
 |                                ||                                          |
 |  o bugfix-login           12m  ||  --- Recent Activity ----------------   |
 |    native . Idle               ||                                          |
 |                                ||  14:49  Edit   auth/middleware.go        |
 |  o token-work            done  ||  14:48  Bash   go test ./auth/...       |
 |    sandbox . Success           ||  14:47  Edit   auth/middleware.go        |
 |                                ||  14:45  Grep   "session.Token"          |
 |                                ||  14:44  Read   auth/session.go          |
 |                                ||                                          |
 +--------------------------------++------------------------------------------+
  up/dn navigate . enter peek . n new . s stop . c clean . ? help . q quit
```

### Design Principles

- **3-line list items**: Name + age, source + status, blank line. Truncate with ellipsis, never wrap.
- **Adaptive colors**: `lipgloss.AdaptiveColor` everywhere for light/dark terminal support.
- **Status via color + symbol**: Colored dot for health, never color alone. Green = healthy, yellow = warning, red = needs attention, gray = idle/completed.
- **Detail pane title tracks selection**: Header shows selected session name.
- **Progress bar only for sandbox sessions**: Native sessions show activity timeline.
- **Dim secondary info**: Source, branch, timestamps in muted gray. Primary info in full foreground.
- **Bottom bar**: Single line, dim gray, grouped with centered dot separators.

### Keybindings

| Key | Action |
|-----|--------|
| j/k or up/down | Navigate session list |
| enter | Toggle peek — detail becomes scrollable log viewport |
| n | New session (prompts type, dir, name inline) |
| s | Stop selected session (inline confirmation) |
| c | Clean completed/failed sessions |
| l | Label selected session |
| ? | Toggle full help overlay |
| q | Quit cs (sessions keep running) |

## CLI Subcommands

All subcommands work without the TUI for scripting and quick actions.

| Command | Description |
|---------|-------------|
| `cs` | Launch TUI dashboard (default) |
| `cs new [--sandbox\|--interactive] [--dir PATH] [--name NAME]` | Create new session |
| `cs ls [--json]` | List sessions (table or JSON) |
| `cs peek <session>` | Tail session log output |
| `cs stop <session>` | Stop a running session |
| `cs label <session> "description"` | Set session task label |
| `cs clean [--all]` | Remove completed/failed sessions |

Session resolution follows claude-sandbox's pattern: accepts ID, name, or interactive picker.

## Project Structure

```
claude-session-manager/
  cmd/cs/
    main.go                  # Entry point, Cobra root command
  internal/
    scanner/
      scanner.go             # Scan() -> []Session, unified discovery
      sandbox.go             # Read .claude-sandbox/sessions/*.json + logs
      native.go              # Read ~/.claude/sessions/*.json + process checks
      health.go              # Heuristic engine, produces []Diagnostic
      scanner_test.go
    tui/
      app.go                 # Root Bubbletea model, tick sub, layout composition
      sessions.go            # Left pane: session list component
      detail.go              # Right pane: detail + peek viewport
      statusbar.go           # Bottom bar: keybinding hints
      styles.go              # All Lipgloss styles (adaptive colors)
      keys.go                # Keybinding definitions
    session/
      session.go             # Session, Diagnostic, Source, Status, Health types
  go.mod
  go.sum
  Makefile
  .goreleaser.yml
```

## Dependencies

| Dependency | Purpose |
|------------|---------|
| github.com/spf13/cobra | CLI framework |
| github.com/charmbracelet/bubbletea/v2 | TUI framework |
| github.com/charmbracelet/bubbles/v2 | Table, viewport components |
| github.com/charmbracelet/lipgloss/v2 | Terminal styling |

Four dependencies. No database, no web framework, no config library.

## Go Patterns (DRIVEC Compliance)

- **Types** in `internal/session/`: shared by scanner and TUI, no circular imports
- **Interfaces**: `SessionSource` with single method `Scan(ctx context.Context) ([]Session, error)`, defined near consumer in scanner.go. Sandbox and native are implementations.
- **Context**: first param everywhere, propagated from TUI tick commands through scanner
- **Errors**: `fmt.Errorf("scan sandbox sessions: %w", err)` — action stated directly, no "failed to" prefix
- **Testing**: table-driven with `t.Run()`, fixture-based, no mocks for file I/O

## Testing Strategy

Tests must be honest quality gates that prove the tool works. If `make verify` passes, the tool functions correctly. If the tool doesn't work, `make verify` must fail.

### Scanner Tests

Fixture-based against real file structures. `testdata/` directories with actual `.claude-sandbox/sessions/*.json`, stream-JSON log excerpts, and `~/.claude/sessions/*.json` samples. Create real directory trees in `t.TempDir()`. Table-driven with `t.Run()` covering discovery, status detection, health triggers, and edge cases (missing files, corrupt JSON, stale PIDs).

### Health Heuristic Tests

Each heuristic gets its own test table. Feed real log fixture data, assert expected diagnostics — signal, severity, and detail message. Use actual stream-JSON excerpts from real sandbox sessions as fixtures.

### TUI Tests

Bubbletea v2 `tea.Test` utilities: send real key sequences, assert rendered view contains expected content. Focus on state transitions — navigation changes selection, peek toggles viewport, stop triggers confirmation. Not pixel-perfect visual testing.

### CLI Subcommand Tests

End-to-end against real fixture directories. `cs ls` produces expected stdout. `cs label` writes to the expected file. Same `setupTestRepo()` pattern as claude-sandbox.

### Integration Test

Creates fixture sessions across both sources, runs full scanner, asserts the complete `[]Session` output matches expectations including health diagnostics.

### Verification Gate

```bash
make verify  # runs: build + vet + lint + test -race + tidy check
```

Single command. If it passes, the tool works. No `t.Skip()` without documented reason. No "TODO: add assertions" stubs.

## Distribution

- GoReleaser for multi-arch binaries (darwin/linux, amd64/arm64)
- Homebrew tap
- GitHub Actions CI: build, test, lint, tidy check
- Pre-commit hooks: build + lint + tidy
- Apache 2.0 license

## Integration Points

### claude-sandbox

`cs` reads claude-sandbox session state directly from `.claude-sandbox/sessions/*.json` and logs from `~/.claude/sandbox-sessions/<id>.log`. No API coupling — pure file-based integration. `cs new --sandbox` can delegate to `claude-sandbox spec` for the creation flow.

### Native Claude Sessions

Reads `~/.claude/sessions/*.json` for active session discovery. Process liveness checked via OS signals.

### Future: Hook-Driven Events (v2)

Architecture supports replacing the polling scanner with a PostToolUse hook that feeds events in real-time. Scanner interface stays the same — implementations swap from file-polling to event-stream consumption. TUI layer unchanged.

### Future: Dashboard Integration

Scanner layer has no TUI dependency. A web API or Electron dashboard can import and call the same `scanner.Scan()` function to get session state, health, and diagnostics. The data model is the integration point.
