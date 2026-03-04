# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- Nothing yet.

---

## [0.1.0] - 2025-01-01

### Added
- `logr watch` command with interactive Bubbletea TUI for real-time log tailing.
- `logr tail` command for plain-text, pipe-friendly log output.
- `logr query` command to search historical logs by time range, level, and regex.
- `logr stats` command for error-rate summaries.
- **Docker source** — tail all running containers or specific docker-compose services via `--docker` / `--service`.
- **Kubernetes source** — tail pods by namespace and label selector via `--kube` / `--namespace` / `--label`.
- **File source** — watch local log files with glob support via `--file`.
- **Stdin source** — auto-detected when input is piped; compatible with `kubectl logs`, `stern`, and any other tool.
- Fan-in multiplexer (`internal/fanin`) to merge multiple log streams into one ordered channel.
- Log filtering by level (`--level`), regex grep (`--grep`), time range (`--last`, `--since`).
- Output formatters: `pretty` (default, colored), `json` (newline-delimited), `logfmt` (key=value), `table` (aligned).
- TUI keybindings: `/` filter, `e` errors-only, `s` sidebar, `g`/`G` scroll, `?` help, `q` quit.
- Embeddable structured logger package (`github.com/OpenCortex-Labs/logr/logr`) writing logfmt output.
- Watch-mode integration helper (`github.com/OpenCortex-Labs/logr/run`) to co-launch app and TUI.
- GitHub Actions CI: test (with race detector) and golangci-lint on every push and PR.

[Unreleased]: https://github.com/OpenCortex-Labs/logr/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/OpenCortex-Labs/logr/releases/tag/v0.1.0
