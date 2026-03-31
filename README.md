# cs — Claude Session Manager

[![CI](https://github.com/dakaneye/claude-session-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/dakaneye/claude-session-manager/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/dakaneye/claude-session-manager)](https://goreportcard.com/report/github.com/dakaneye/claude-session-manager)
[![Go Reference](https://pkg.go.dev/badge/github.com/dakaneye/claude-session-manager.svg)](https://pkg.go.dev/github.com/dakaneye/claude-session-manager)
[![Release](https://img.shields.io/github/v/release/dakaneye/claude-session-manager)](https://github.com/dakaneye/claude-session-manager/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

TUI + CLI for managing multiple Claude Code sessions. See what each session is doing, catch when one is stuck, and context-switch without losing track.

## Why

Managing 3-5 concurrent Claude Code sessions is painful. You lose track of what each is doing, can't tell which are stuck, and switching is manual. Existing tools (amux, claude-squad, ccmanager) require tmux, have fragile ANSI parsing, or lack visibility into session health.

`cs` fills the gap: no tmux dependency, session state visibility, peek into active sessions, and stuck/anomaly detection.

## Features

- **Split-pane TUI dashboard** with session list and detail view
- **Unified view** of both interactive (native) and autonomous ([claude-sandbox](https://github.com/dakaneye/claude-sandbox)) sessions
- **Health heuristics** — detects repeated edits, test loops, idle sessions, repeated commands, high context window usage
- **CLI subcommands** for scripting (`cs ls --json`, `cs peek`, `cs stop`)
- **No tmux dependency** — pure Go, single binary

## Install

### Go

```bash
go install github.com/dakaneye/claude-session-manager/cmd/cs@latest
```

### Binary

Download from [Releases](https://github.com/dakaneye/claude-session-manager/releases/latest).

## Usage

### TUI Dashboard

```bash
cs
```

Navigate with `j/k`, press `enter` to peek at session logs, `?` for help, `q` to quit. Sessions keep running when you exit.

### CLI

```bash
cs ls                          # List all sessions
cs ls --json                   # JSON output for scripting
cs peek <session>              # Tail session activity log
cs stop <session>              # Stop a session
cs label <session> "desc"      # Label a session
cs clean                       # Remove completed/failed sessions
cs new --sandbox               # Start a sandbox session
cs new                         # Start an interactive session
```

### Session Sources

`cs` discovers sessions from two sources:

- **Native Claude sessions** (`~/.claude/sessions/`) — interactive sessions you're driving
- **claude-sandbox sessions** (`.claude-sandbox/sessions/`) — autonomous sessions running in containers

### Health Detection

Sessions get green/yellow/red health indicators based on:

| Signal | Yellow | Red |
|--------|--------|-----|
| Same file edited repeatedly | 10+ times | 15+ times |
| Tests failing in loop | — | 3+ consecutive |
| No activity | 5 min | 10 min |
| Context window usage | 65%+ | 75%+ |
| Identical command repeated | 3 in a row | — |

## Development

```bash
git clone https://github.com/dakaneye/claude-session-manager.git
cd claude-session-manager
make build        # Build the binary
make test         # Run tests with race detector
make lint         # Run golangci-lint
make verify       # Full verification (build + vet + lint + test + tidy)
```

## License

[MIT](LICENSE)
