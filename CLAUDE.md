# Claude Development Guidelines for claude-session-manager

## Project Overview

Go TUI + CLI (`cs`) for managing multiple Claude Code sessions with health heuristic anomaly detection.

## Architecture

```
cmd/cs/           Cobra CLI entry point + subcommands
internal/
  session/        Shared types (Session, Diagnostic, Source, Status, Health)
  scanner/        Session discovery, log parsing, health heuristics
  tui/            Bubbletea v2 TUI (styles, keys, sessions list, detail, statusbar, app)
```

Scanner layer has no TUI imports. TUI consumes scanner output.

## Quality Gates

Before committing, all gates must pass:

```bash
# Single command — if it passes, the tool works
make verify    # build + vet + lint (0 issues) + test -race + tidy
```

## Testing Requirements

- Tests must be honest quality gates that prove the tool works
- Test against real file structures and fixtures, not minimal mocks
- CLI tests assert actual stdout output, not just "no error"
- TUI tests assert rendered content contains expected strings
- No `t.Skip()` without documented reason
- No "TODO: add assertions" stubs
- Run ALL tests (integration and e2e) after every change

## Key Patterns

- Use `cmd.Println()` / `cmd.PrintErrf()` for CLI output, not `fmt.Print`
- Use `findRepoRoot()` pattern if needed for commands requiring repo context
- Session resolution: accepts ID or name
- Error wrapping: `fmt.Errorf("action: %w", err)` — no "failed to" prefix
- Context: first param, propagated everywhere

## Dependencies

3 direct dependencies:
- `charm.land/bubbletea/v2` — TUI framework
- `charm.land/lipgloss/v2` — Terminal styling
- `github.com/spf13/cobra` — CLI framework

## Commit Format

`<type>(<scope>): <subject>` — imperative mood, no caps, no period, max 50 chars.
Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert.
