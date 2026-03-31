# Contributing

## Development

```bash
git clone https://github.com/dakaneye/claude-session-manager.git
cd claude-session-manager
make build    # Build binary to bin/cs
make test     # Run tests with race detector
make lint     # Run golangci-lint
make verify   # Full check: build + vet + lint + test + tidy
```

## Before Submitting

1. `make verify` passes
2. New functionality has tests
3. Commit messages follow conventional commits (`feat:`, `fix:`, `test:`, etc.)

## Pull Requests

- Keep changes focused
- Update tests for new functionality
- Follow existing code style
