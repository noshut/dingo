# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

dingo is a DynDNS client written in Go, deployed as a long-running Docker container. It detects the public IPv4/IPv6 via HTTP echo services and keeps A/AAAA records in sync through DNS provider APIs. It also targets providers **without native DynDNS support** (e.g. Hetzner), so updates are done as regular record create/update calls. It replaces an earlier Python implementation.

## Commands

```bash
make build                                  # -> bin/dingo
make test                                   # go test -race ./...
make lint                                   # go vet + gofmt check
go test ./internal/ipdetect -run TestDetect # single test
go run ./cmd/dingo -config config.yaml -once  # one sync run, then exit
make docker                                 # docker build -t dingo:latest .
```

CI (`.github/workflows/ci.yml`) runs the jobs `lint` (`go mod tidy -diff`, `make lint`, golangci-lint v2.1.6), `test` (`make test`, `make build`) and `docker` (image build). These job names are required checks for auto-merge, so don't rename them. Run `golangci-lint run ./...` locally before pushing.

Without `-once`, the binary runs as a daemon (default). The config path comes from `-config`, falling back to `$DINGO_CONFIG` and then `/etc/dingo/config.yaml`.

## Architecture

The flow is `cmd/dingo/main.go` → `config` → `provider` instances → `updater` loop, which uses `ipdetect`.

- **`internal/config`**: YAML config. `${VAR}` is expanded via `os.ExpandEnv` **before** parsing, so secrets (API tokens) come from the container environment. `providers` is a map of *named instances* (`type` + free-form `settings`). Each record references an instance by name, so several accounts of the same provider type are possible. See `config.example.yaml`.
- **`internal/provider`**: `Provider` interface with a single `Upsert(ctx, Record, netip.Addr)`. Implementations must be idempotent (create if missing, update if different, otherwise no-op). Providers self-register in `init()` via `provider.Register("<type>", factory)`.
- **Adding a provider**: create `internal/provider/<name>/`, call `Register` in `init()`, and add a blank import in `cmd/dingo/main.go`. Nothing else needs changing.
- **`internal/ipdetect`**: one `Detector` per IP family. Its HTTP client is pinned to `tcp4`/`tcp6`, so the family is guaranteed on dual-stack hosts. Sources are tried in order (fallback), and answers of the wrong family are rejected.
- **`internal/updater`**: each cycle detects every needed family at most once and calls `Upsert` only when the address differs from the last successful update (in-memory cache, so every record is upserted once after a restart). Errors are logged per record and do not abort the cycle. Shutdown happens on SIGINT/SIGTERM through the context.

## Conventions

- Logging uses `log/slog` (text handler to stdout), with structured key/value pairs only.
- External dependencies are kept minimal (currently only `gopkg.in/yaml.v3`). Provider APIs use `net/http` directly, not vendor SDKs.
- Container: distroless static, nonroot, `CGO_ENABLED=0`. IPv6 detection inside Docker needs `network_mode: host` or an IPv6-enabled network (see `docker-compose.yml`).

## Status

The skeleton is in place. The Hetzner provider (`internal/provider/hetzner`) is a stub that returns "not implemented".
