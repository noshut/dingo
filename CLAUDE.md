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

CI (`.github/workflows/ci.yml`) runs the jobs `lint` (`go mod tidy -diff`, `make lint`, golangci-lint v2.1.6), `test` (`make test`, `make build`) and `docker` (image build). These job names are required checks for auto-merge, so don't rename them. Run `golangci-lint run ./...` locally before pushing. `main` is protected by a ruleset: changes only land via PR with green `lint`/`test`/`docker`, and auto-merge is enabled.

Release (`.github/workflows/release.yml`) pushes multi-arch images (amd64, arm64, arm/v7) to Docker Hub `karlender/dingo`. A push to `main` publishes `:edge` and `:sha-<short>`. A `vX.Y.Z` tag publishes `:X.Y.Z`, `:X.Y`, `:X` (not for v0.x) and `:latest`, and creates a GitHub Release with generated notes. This needs the repo secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`. The version is injected via `-X main.version` (build-arg `VERSION`) and shown by `dingo -version`. The Dockerfile cross-compiles on `$BUILDPLATFORM` (no QEMU), so keep the final stage free of `RUN`.

Without `-once`, the binary runs as a daemon (default). The config path comes from `-config`, falling back to `$DINGO_CONFIG` and then `/etc/dingo/config.yaml`.

## Architecture

The flow is `cmd/dingo/main.go` → `config` → `provider` instances → `updater` loop, which uses `ipdetect`.

- **`internal/config`**: YAML config. `${VAR}` is expanded via `os.ExpandEnv` **before** parsing, so secrets (API tokens) come from the container environment. `providers` is a map of *named instances* (`type` + free-form `settings`). Each record references an instance by name, so several accounts of the same provider type are possible. See `config.example.yaml`.
- **`internal/provider`**: `Provider` interface with a single `Upsert(ctx, Record, netip.Addr) (changed bool, err error)`. Implementations must be idempotent (create if missing, update if different, otherwise no-op) and report whether they wrote anything. The updater logs `record updated` (info) only when `changed` is true. Providers self-register in `init()` via `provider.Register("<type>", factory)`.
- **Adding a provider**: create `internal/provider/<name>/`, call `Register` in `init()`, and add a blank import in `cmd/dingo/main.go`. Nothing else needs changing.
- **`internal/ipdetect`**: one `Detector` per IP family. Its HTTP client is pinned to `tcp4`/`tcp6`, so the family is guaranteed on dual-stack hosts. Sources are tried in order (fallback), and answers of the wrong family are rejected.
- **`internal/updater`**: each cycle detects every needed family at most once and calls `Upsert` only when the address differs from the last successful update (in-memory cache, so every record is upserted once after a restart). Errors are logged per record and do not abort the cycle. Shutdown happens on SIGINT/SIGTERM through the context.

## Conventions

- Logging uses `log/slog` through `internal/logging`, with structured key/value pairs only. `main` installs the logger as `slog.Default()`, and packages log via `slog.Debug(...)` etc. The level comes from `-debug`, then `-log-level`/`DINGO_LOG_LEVEL`, then config `log_level`, else `info`.
- **Secrets must never be logged.** The handler redacts attributes with sensitive keys (`token`, `password`, `authorization`, ...) and scrubs registered secret values (provider settings with sensitive keys) from all strings and errors. Never log HTTP headers or request bodies. `config.Load` masks backtick-quoted values in yaml errors, because they can contain expanded `${ENV}` secrets. Log URLs only via `ipdetect.RedactURL`, which masks userinfo and query values.
- External dependencies are kept minimal (currently only `gopkg.in/yaml.v3`). Provider APIs use `net/http` directly, not vendor SDKs.
- Container: distroless static, nonroot, `CGO_ENABLED=0`. IPv6 detection inside Docker needs `network_mode: host` or an IPv6-enabled network (see `docker-compose.yml`).

## Providers

- **hetzner**: uses the DNS part of the Hetzner Cloud API (`api.hetzner.cloud/v1`, *not* the legacy `dns.hetzner.com`), with a Cloud project token (read & write). Upsert does `GET /zones/{zone}/rrsets/{name}/{type}`, then on `not_found` it calls `POST /zones/{zone}/rrsets`. Otherwise it calls `actions/set_records` and/or `actions/change_ttl`. dingo **owns** each managed RRSet: `set_records` replaces all values, which is intended. Every write returns an Action that is polled via `GET /zones/actions/{id}` until it is no longer `running`. The apex is written as `@`, and names must be lowercase. Tests use an in-memory fake API (`settings.endpoint` overrides the base URL). The OpenAPI spec is at `https://docs.hetzner.cloud/cloud.spec.json`.
