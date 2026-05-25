<div align="center">

# goboxd

**A Go HTTP service for executing untrusted code in isolated sandboxes.**

[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23-00ADD8.svg?logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Required-2496ED.svg?logo=docker&logoColor=white)](https://www.docker.com)

</div>

---

## Overview

goboxd is a high-performance HTTP service written in Go that compiles and runs untrusted user code inside isolated sandboxes, returning execution metrics and test case output. It leverages process isolation, bounded concurrency queueing, and a modular runtime design to process untrusted code safe from resource starvation and container failures.

## Features

- **Plug and Play Language Registry**: Runtimes are configured dynamically using a modular YAML schema (`config/languages.yaml`), allowing immediate addition of compilers or interpreters.
- **Process Isolation**: Enforces sandbox isolation using Linux namespaces, chroot mount policies, and cgroups (via `nsjail`).
- **Semaphore Concurrency Throttling**: Protects the host system under heavy load by queueing execution requests through an internal 16-worker semaphore channel, preventing process fork exhaustion (`nproc` limit triggers) and system OOM crashes.
- **Decoupled Asynchronous Garbage Collection**: Offloads orphaned directory cleanup to a single background worker running on a 5-minute ticker, preventing disk lock contention during request processing.
- **Native Load Generator Script**: Includes a local stress testing tool (`scripts/stress.go`) built with native Go concurrency. It supports batch testing across concurrency tiers (P1, P10, P20, P50, and P100 concurrent workers) and tracks throughput (RPS), execution failures, and latency percentiles (P50, P90, P99).
- **Resource Constraints**: Custom execution bounds for memory limits, execution wall-time thresholds, and thread/process limits on a per-request basis.
- **Telemetry and Readiness Probes**: Implements endpoints for `/healthz`, `/readyz` (active compiler smoke probing), and `/info` (exposing active in-flight jobs, throughput, and compilation specs).

## Getting started

### Prerequisites

- Docker with Compose v2
- Go 1.22+ (only required if running the load generator script locally)

No Go toolchain or system dependencies are required on the host to run the server. Everything runs inside containerized environments.

### Installation

```sh
git clone https://github.com/thesouldev/goboxd.git
cd goboxd
make build
```

### Usage

```sh
make run          # Start the service on :8080
make test         # Run core unit tests
make integration  # Run containerized integration tests
make lint         # Run golangci-lint check
```

### Stress Testing

To stress test the active service under concurrent workloads:
```sh
# Run batch suite (P1 -> P100 concurrent workers) for 5 seconds per tier
go run scripts/stress.go --payload test_py3.json --batch --duration 5s
```

## Project structure

```
.
├── cmd/goboxd/   binary entry point
├── config/       language YAML configurations
├── internal/     private application packages (sandbox, config, types)
├── scripts/      high-performance native load testing scripts
└── tests/        integration tests
```

## Contributing

Contributions are welcome. Open an issue to discuss substantial changes before sending a pull request.

## License

This project is distributed under the GNU General Public License v3.0. See [LICENSE](LICENSE) for the full text.

---

### FRAMEWORK
HTTP was chosen because it is the standard web protocol, making it extremely easy to connect directly to browsers, code playgrounds, and frontend clients.
