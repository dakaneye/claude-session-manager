# PTY-Based Session Attach/Detach

**Date:** 2026-03-31
**Issue:** #6 — Explore tmux-based session attach for interactive sessions
**Decision:** Built-in PTY proxy via `charmbracelet/x/xpty` instead of tmux

## Problem

`cs` can monitor Claude Code sessions but can't attach to them interactively. The `a` key shows a flash message with directory and PID — not actionable. Without a terminal multiplexer or PTY ownership, you can't take over another process's TTY.

## Approach

`cs` becomes a session owner, not just an observer. Sessions launched via `cs new` are spawned with a PTY that `cs` holds. This enables full bidirectional attach (connect your terminal to the PTY) and detach (disconnect, return to TUI) while the session keeps running.

### Why not tmux?

tmux as a programmatic substrate has documented fragility:

- User `.tmux.conf` breaks wrapper assumptions (pane-base-index, prefix key conflicts)
- `capture-pane` fails silently across macOS versions (claude-squad #51, #189, #216)
- Terminal emulator incompatibility (iTerm2 tmux -CC mode, Warp)
- ANSI escape corruption in text-based protocol

The PTY approach avoids this surface area entirely. `tea.ExecProcess` provides native suspend/resume in Bubbletea. All PTY operations compile into the single binary — no external dependencies.

Fallback: if the PTY approach proves flaky, tmux remains a viable pivot.

## Architecture

```
cmd/cs/              CLI commands (new, attach, resume, sandbox subcommands)
internal/
  pty/               PTY lifecycle: spawn, attach, detach, resize, cleanup
  scanner/           Session discovery — now also discovers cs-managed sessions
  session/           Shared types — extended with PTY/managed metadata
  tui/               Bubbletea TUI — uses tea.ExecProcess for attach
```

### Managed vs Discovered Sessions

**Managed sessions** (launched via `cs new`): `cs` holds the PTY fd. Full attach/detach/resume support.

**Discovered sessions** (launched in another terminal): Scanner finds them via `~/.claude/sessions/*.json` as before. Peek, activity, health monitoring all work. Attach shows "Session not managed by cs — use peek to monitor."

## PTY Lifecycle

### Spawn (`cs new`)

1. Allocate PTY via `xpty.NewPty(width, height)`
2. Start `claude` (or `claude-sandbox spec` for sandbox) attached to PTY
3. Write metadata to `~/.claude/cs-sessions/<id>.json`: PID, PTY path, working dir, created timestamp, session ID
4. Background goroutine consumes PTY stdout and buffers recent output
5. Return to TUI — session appears in list as managed, status "running"

### Attach (`a` key or `cs attach <session>`)

1. Verify session is managed and alive
2. TUI calls `tea.ExecProcess` with custom `ExecCommand`:
   - Terminal enters raw mode
   - Proxies stdin → PTY input, PTY output → stdout
   - Listens for detach chord (`Ctrl+]`)
   - Propagates SIGWINCH to `xpty.Resize()`
3. TUI suspends — user is full-screen in the Claude session

### Detach (`Ctrl+]`)

1. Proxy stops forwarding stdin/stdout
2. `ExecCommand.Run()` returns
3. `tea.ExecProcess` callback fires, TUI resumes
4. Session keeps running — PTY goroutine continues buffering output

### Session Exit

1. PTY EOF detected by background goroutine
2. Session metadata updated (status → completed/failed)
3. Next TUI tick picks up state change
4. If user was attached, proxy detects EOF, returns, TUI resumes

### Resume (`a` key on dead session or `cs resume <session>`)

When a managed session dies (cs crash, SIGHUP, intentional stop):

1. Orphaned metadata found in `~/.claude/cs-sessions/` — PID dead, no PTY
2. Session shows in list with status `stopped`
3. User presses `a` → `cs` spawns `claude --resume <session-id>` on fresh PTY
4. Claude picks up where it left off. Session becomes managed again.

## Session Metadata & Storage

**Directory:** `~/.claude/cs-sessions/`

```json
{
  "id": "a1b2c3",
  "pid": 12345,
  "dir": "/Users/user/dev/project",
  "source": "native",
  "stage": "",
  "created_at": "2026-03-31T10:00:00Z",
  "managed": true
}
```

Sandbox sessions include stage tracking:

```json
{
  "id": "d4e5f6",
  "pid": 12346,
  "dir": "/Users/user/dev/project",
  "source": "sandbox",
  "stage": "spec",
  "stage_history": [
    {"stage": "spec", "started_at": "...", "finished_at": "..."}
  ],
  "created_at": "2026-03-31T10:00:00Z",
  "managed": true
}
```

**Deduplication:** Managed sessions also appear in `~/.claude/sessions/`. Scanner deduplicates by PID — managed version wins (has PTY handle).

**Cleanup:** `cs clean` removes stale managed session files for dead processes.

## Sandbox Lifecycle

Sandbox sessions advance through three stages, each on its own PTY:

1. `cs new --sandbox` → spawns `claude-sandbox spec` on PTY #1. Status: `speccing`
2. Spec exits → status: `ready`. User presses `a` → prompted to start execute
3. `cs sandbox execute <session>` → spawns `claude-sandbox execute` on PTY #2. Status: `executing`
4. Execute exits → status: `completed`. User presses `a` → prompted to start ship
5. `cs sandbox ship <session>` → spawns `claude-sandbox ship` on PTY #3. Status: `shipping`
6. Ship exits → session done

Resume works within stages — if `execute` dies mid-run, resume picks it back up.

## CLI & TUI Changes

### New CLI Commands

| Command | Purpose |
|---------|---------|
| `cs attach <session>` | Full-screen PTY proxy with `Ctrl+]` detach |
| `cs resume <session>` | Resume a dead/stopped session on new PTY |
| `cs sandbox execute <session>` | Advance sandbox to execute stage |
| `cs sandbox ship <session>` | Advance sandbox to ship stage |

### Modified CLI Commands

| Command | Change |
|---------|--------|
| `cs new` | Spawns via PTY instead of raw exec |
| `cs new --sandbox` | Spawns `claude-sandbox spec` via PTY |
| `cs ls` | Shows managed indicator for attach-capable sessions |
| `cs stop` | Also closes PTY fd for managed sessions |

### TUI Key Behavior

The `a` key becomes context-sensitive:

| Session State | Action |
|--------------|--------|
| Active PTY (running) | Attach via `tea.ExecProcess` |
| Dead managed session | Resume via `claude --resume` on new PTY |
| Dead discovered session | Resume via `claude --resume` on new PTY (becomes managed) |
| Between sandbox stages | Prompt to start next stage |
| Running discovered session | Flash "not managed — use peek to monitor" |

All other TUI behavior unchanged: detail pane, peek, activity, health, labels.

## Dependencies

**New:** `charmbracelet/x/xpty` — pinned to commit hash (pre-release, no stable tags yet)

**Existing (unchanged):**
- `charm.land/bubbletea/v2`
- `charm.land/lipgloss/v2`
- `github.com/spf13/cobra`

## Known Issues & Workarounds

| Issue | Severity | Mitigation |
|-------|----------|------------|
| `tea.ExecProcess` first-keypress bug (bubbletea #1116) | Minor | Document. Investigate workaround. |
| Alt-screen output leak (bubbletea #431) | Cosmetic | Return empty `View()` before exec |
| `xpty` API changes (pre-release) | Moderate | Pin to commit hash |
| `cs` crash kills managed sessions | By design | Resume via `claude --resume`. Same as closing a terminal tab. |
| Detach chord `Ctrl+]` conflict | Low | Make configurable via env var |

## Testing Strategy

### Unit Tests (`internal/pty/`)

- Spawn simple command via PTY, verify output received
- Write to PTY stdin, verify echo back
- Detach and verify process stays alive
- Session exit produces EOF
- Resize propagation

### Integration Tests

- `cs new` creates managed session file in `~/.claude/cs-sessions/`
- Scanner discovers managed sessions and deduplicates against native
- `cs attach` connects to managed session, `Ctrl+]` returns cleanly
- `cs attach` on discovered session errors with clear message
- `cs stop` closes PTY and kills process
- `cs clean` removes stale managed session files
- `cs resume` spawns `claude --resume` on fresh PTY

### TUI Tests

- `a` key on managed session triggers `tea.ExecProcess`
- `a` key on discovered session shows flash message
- `a` key on dead managed session triggers resume
- `a` key between sandbox stages prompts next stage
- Session list renders managed indicator

Tests use simple commands (`cat`, `echo`, sleep scripts) as subprocess stand-ins — not Claude itself.

## Out of Scope (YAGNI)

- Session durability across `cs` restarts (sessions die with `cs`)
- Inline terminal pane in detail view (stretch goal, not v1)
- Attach for discovered (non-managed) sessions
- Multiple simultaneous attaches to one session
- GUI / desktop app
