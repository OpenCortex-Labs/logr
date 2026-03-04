# logr

Terminal-native log intelligence for developers. Tail, filter, and visualize logs from Docker, Kubernetes, local files, and stdin — without any infrastructure.

> The observability tool for teams too small for Datadog and too serious for grep.

[![CI](https://github.com/OpenCortex-Labs/logr/actions/workflows/ci.yml/badge.svg)](https://github.com/OpenCortex-Labs/logr/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/OpenCortex-Labs/logr)](https://goreportcard.com/report/github.com/OpenCortex-Labs/logr)
[![Go Reference](https://pkg.go.dev/badge/github.com/OpenCortex-Labs/logr.svg)](https://pkg.go.dev/github.com/OpenCortex-Labs/logr)
[![GitHub Release](https://img.shields.io/github/v/release/OpenCortex-Labs/logr)](https://github.com/OpenCortex-Labs/logr/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Install

```bash
# Homebrew
brew install OpenCortex-Labs/tap/logr

# Go install
go install github.com/OpenCortex-Labs/logr/cmd/logr@latest

# curl (Linux / macOS)
curl -sf https://raw.githubusercontent.com/OpenCortex-Labs/logr/main/install.sh | sh
```

Or grab a binary from the [Releases](https://github.com/OpenCortex-Labs/logr/releases) page.

---

## Usage

```bash
# Interactive TUI — watch all docker-compose services
logr watch --docker

# Watch specific services
logr watch --docker --service api,worker

# Watch Kubernetes pods
logr watch --kube --namespace production --label app=api

# Watch local log files (glob supported)
logr watch --file ./logs/*.log

# Pipe from any tool
kubectl logs -f pod/api-xyz | logr watch
stern api | logr watch

# Plain text tail (pipe-friendly)
logr tail --docker --level error
logr tail --file app.log --grep "panic" --output json

# Search historical logs
logr query --file app.log --last 1h --level error --output table

# Error rate summary
logr stats --docker --last 1h
```

---

## TUI Keybindings

| Key | Action |
|-----|--------|
| `/` | Open filter bar |
| `esc` | Close filter bar |
| `e` | Toggle errors-only mode |
| `s` | Toggle sidebar |
| `g` / `G` | Scroll to top / bottom |
| `?` | Toggle help |
| `q` | Quit |

---

## Output Formats

```bash
logr tail --docker --output pretty   # default, colored
logr tail --docker --output json     # newline-delimited JSON
logr tail --docker --output logfmt   # key=value pairs
logr tail --docker --output table    # aligned table
```

---

## Global Flags

```
--level     error|warn|info|debug
--grep      string match (regex supported)
--last      duration (1h, 30m, 7d)
--since     RFC3339 timestamp
--no-color  disable color output
--output    pretty|json|logfmt|table
```

---

## Sources

| Flag | Description |
|------|-------------|
| `--docker` | All running Docker containers |
| `--docker --service api,worker` | Specific docker-compose services |
| `--kube` | Kubernetes pods (default namespace) |
| `--kube --namespace prod --label app=api` | Filtered k8s pods |
| `--file ./logs/*.log` | Local files with glob support |
| stdin (auto-detected when piped) | Any piped input |

---

## Embeddable Logger

logr ships a structured logger you can embed in your Go app. Logs are written in logfmt so the logr CLI can parse and filter them.

```go
import "github.com/OpenCortex-Labs/logr"

logr.Info("server started")
logr.Errorf("failed to connect: %v", err)

// Scoped logger with fields
log := logr.Default.With("request_id", id)
log.Info("request received")
```

For watch-mode integration (auto-start TUI + app together):

```go
import "github.com/OpenCortex-Labs/logr/run"

func main() {
    run.New(api.Run).Start()
}
```

```bash
# TUI watches your app's log file live
./myapp watch
```

---

## Contributing

Contributions are welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for how to get started, the project structure, and how to add new log sources.

---

## Community

| | |
|---|---|
| Contributing | [CONTRIBUTING.md](CONTRIBUTING.md) |
| Code of Conduct | [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) |
| Security Policy | [SECURITY.md](SECURITY.md) |
| Changelog | [CHANGELOG.md](CHANGELOG.md) |
| Releases | [GitHub Releases](https://github.com/OpenCortex-Labs/logr/releases) |
| Discussions | [GitHub Discussions](https://github.com/OpenCortex-Labs/logr/discussions) |

---

## License

[MIT](LICENSE)
