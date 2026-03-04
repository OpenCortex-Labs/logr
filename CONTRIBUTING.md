# Contributing to logr

Thanks for your interest in contributing! logr is a terminal-native log intelligence tool for developers, and contributions of all kinds are welcome.

## Getting Started

### Prerequisites

- Go 1.21+
- Docker (optional, for testing the Docker source)
- A Kubernetes cluster or `minikube` (optional, for testing the Kubernetes source)

### Setup

```bash
git clone https://github.com/OpenCortex-Labs/logr.git
cd logr
go mod download
go build ./...
```

Run the CLI locally:

```bash
go run ./cmd/logr watch --help
```

## Project Structure

```
cmd/logr/          # CLI entrypoint
internal/
  cli/             # Cobra commands and flag parsing
  fanin/           # Fan-in multiplexer for log sources
  filter/          # Log entry filtering logic
  source/          # Log sources: Docker, Kubernetes, file, stdin
  tui/             # Bubbletea TUI (watch command)
pkg/
  output/          # Output formatters: pretty, JSON, logfmt, table
logr/              # Embeddable logger package
```

## Making Changes

1. **Fork** the repo and create a branch from `main`:
   ```bash
   git checkout -b fix/docker-reconnect
   ```

2. **Make your changes.** Keep commits focused and atomic.

3. **Test your changes:**
   ```bash
   go vet ./...
   go test ./... -race
   ```

4. **Open a pull request** against `main`. Fill out the PR template.

## Adding a New Log Source

1. Implement the `source.Source` interface in `internal/source/`:
   ```go
   type Source interface {
       Name() string
       Stream(ctx context.Context) (<-chan LogEntry, error)
       Close() error
   }
   ```

2. Wire it up in `internal/cli/root.go` inside `resolveSources()`.

3. Add a `--yourflag` source flag via `addSourceFlags()`.

## Commit Style

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add syslog source
fix: handle docker reconnect on container restart
docs: update kubernetes usage example
chore: bump bubbletea to v0.26
```

Commits starting with `docs:`, `test:`, or `chore:` are excluded from the changelog automatically.

## Reporting Bugs

Use the [Bug Report](.github/ISSUE_TEMPLATE/bug_report.yml) issue template. Please include your OS, logr version (`logr version`), and the log source you were using.

## Security

If you discover a security vulnerability, **do not open a public issue**. Please follow the process described in [SECURITY.md](SECURITY.md) to report it privately.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior via the contact method listed in that document.
