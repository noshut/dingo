<p align="center">
  <img src="docs/assets/dingo.webp" alt="dingo – DynDNS gopher" width="360">
</p>

<h1 align="center">dingo</h1>

<p align="center">
  DynDNS client written in Go. It keeps A/AAAA records in sync with your public IP,<br>
  including DNS providers <b>without native DynDNS support</b> such as Hetzner.
</p>

<p align="center">
  <a href="https://github.com/noshut/dingo/actions/workflows/ci.yml"><img src="https://github.com/noshut/dingo/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/noshut/dingo/actions/workflows/release.yml"><img src="https://github.com/noshut/dingo/actions/workflows/release.yml/badge.svg" alt="Release"></a>
  <a href="https://hub.docker.com/r/karlender/dingo"><img src="https://img.shields.io/docker/pulls/karlender/dingo" alt="Docker Pulls"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/noshut/dingo" alt="License"></a>
</p>

## Features

- **Multi-provider**: several providers and accounts in one config. Each record picks its provider.
- **IPv4 and IPv6**: A and AAAA records. The IP is detected separately per address family, with fallback sources.
- **Only writes on change**: the DNS API is only called when the IP changes; the TTL is reconciled on startup.
- **Fast IP change detection**: optionally pings the current public IPv4. When the router stops answering (line down or new IP), a sync runs immediately instead of waiting for the next interval.
- **Health check**: `/healthz` endpoint and a built-in Docker `HEALTHCHECK`.
- **Daemon or one-shot**: runs as a long-lived container, or once with `-once` (e.g. from cron).
- **Debug mode with secret redaction**: `-debug` or `DINGO_LOG_LEVEL=debug`; tokens never reach the log.
- **Small, secure image**: distroless, nonroot, static binary for `amd64`, `arm64` and `arm/v7` (Raspberry Pi).

## Quick start (Docker)

```bash
curl -O https://raw.githubusercontent.com/noshut/dingo/main/config.example.yaml
mv config.example.yaml config.yaml   # adjust zones/records
```

```bash
docker run -d --name dingo --restart unless-stopped --network host \
  -e HETZNER_TOKEN=... \
  -v "$PWD/config.yaml:/etc/dingo/config.yaml:ro" \
  karlender/dingo:latest
```

You can also use [`docker-compose.yml`](docker-compose.yml) with `docker compose up -d`.

> **IPv6:** use `--network host` (or an IPv6-enabled Docker network). Otherwise the container can't see the host's public IPv6.

### Image tags

| Tag | Content |
|-----|---------|
| `latest`, `X.Y.Z`, `X.Y`, `X` | Releases (git tag `vX.Y.Z`) |
| `edge` | Current state of `main` |
| `sha-<commit>` | A specific commit on `main` |

## Configuration

`${VAR}` is expanded from the environment, so tokens belong in env vars and not in the file.

```yaml
interval: 5m                 # check interval (default 5m)
log_level: info              # debug, info, warn, error (default info)

# ip_sources:                # optional, defaults: ipify / icanhazip
#   ipv4: [https://api.ipify.org]
#   ipv6: [https://api6.ipify.org]

providers:
  hetzner-main:              # free-form name, referenced by records
    type: hetzner
    settings:
      token: ${HETZNER_TOKEN}

records:
  - provider: hetzner-main
    zone: example.com
    name: home               # "@" = zone apex
    type: A                  # A or AAAA
    ttl: 300                 # optional, default 300
```

| Flag / env | Default | Description |
|------------|---------|-------------|
| `-config` / `DINGO_CONFIG` | `/etc/dingo/config.yaml` | Path to the config |
| `-once` | – | One sync run, then exit |
| `-log-level` / `DINGO_LOG_LEVEL` | `info` (or `log_level` from the config) | `debug`, `info`, `warn`, `error` |
| `-debug` | – | Shortcut for `-log-level debug` |
| `-healthcheck` | – | Query `/healthz` of the running instance, exit `0` = healthy (used by the Docker `HEALTHCHECK`) |
| `-version` | – | Print version |

Precedence: `-debug` > `-log-level` / `DINGO_LOG_LEVEL` > `log_level` in the config. At debug level, dingo also logs the IP sources, skipped records and every provider API call (method, path, status, duration). Secrets such as tokens are always masked as `[REDACTED]`, including inside error messages.

### Health check

dingo serves `GET /healthz` on `127.0.0.1:9180` (JSON, `200` or `503`). The Docker image has a built-in `HEALTHCHECK` that calls `dingo -healthcheck`, so `docker ps` shows `healthy` / `unhealthy`.

| Status | HTTP | Meaning |
|--------|------|---------|
| `ok` | 200 | Loop runs, all records are in sync (or failed only briefly) |
| `failing` | 503 | A record hasn't synced successfully for longer than `fail_after`. Example: IPv6 detection fails permanently |
| `stalled` | 503 | No sync cycle completed for over 2× `interval` + 2 min |

```yaml
health:
  enabled: true              # default true
  listen: 127.0.0.1:9180     # default; with network_mode: host this is the host's loopback
  fail_after: 15m            # default 3x interval
```

The response contains only status, addresses and timestamps, never error texts or tokens.

### IP watch (optional)

```yaml
watch:
  enabled: true
  interval: 15s   # ping interval (default 15s)
  failures: 2     # consecutive failed pings before a sync is triggered (default 2)
```

The router answers pings to its own public IP locally. If that stops working, the line is down or the IP has changed, so dingo triggers a sync right away. While the IP stays unreachable, further syncs back off exponentially (up to `interval`), so a router that ignores ICMP doesn't cause constant requests. IPv4 only; IPv6 is still checked by the regular interval.

ICMP needs one of the following:
- **Unprivileged ping** (recommended): the host allows ping sockets for the container's group (`65532`). Many distributions already do. Otherwise:
  ```bash
  echo 'net.ipv4.ping_group_range = 0 2147483647' | sudo tee /etc/sysctl.d/99-ping.conf && sudo sysctl --system
  ```
- **Raw sockets**: run as root (`user: "0"`). `cap_add: NET_RAW` alone is not enough for the nonroot image.

If neither is available, dingo logs `ip watch disabled` and keeps running with the regular interval.

## Providers

| Type | API | Token |
|------|-----|-------|
| `hetzner` | [Hetzner Cloud API](https://docs.hetzner.cloud/reference/cloud#tag/zone-rrsets) (DNS in the Hetzner Console) | Console → Project → Security → API tokens, **Read & Write** |

> dingo **owns** every record it manages: on update, the whole RRSet (name + type) is replaced with the current IP. Don't configure names that hold additional, manually managed A/AAAA values.

More providers can be added easily; see [Development](#development).

## Development

Requires Go ≥ 1.24.

```bash
make test     # go test -race ./...
make lint     # go vet + gofmt (CI additionally runs golangci-lint)
make build    # -> bin/dingo
go run ./cmd/dingo -config config.yaml -once
```

**New provider:** create `internal/provider/<name>/`, implement `provider.Provider` (an idempotent `Upsert`), call `provider.Register("<name>", New)` in `init()`, and add a blank import in `cmd/dingo/main.go`.

Changes to `main` go only through pull requests with green CI (`lint`, `test`, `docker`). Releases are created by a tag:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

This publishes the Docker image and creates a GitHub Release with generated release notes.

## License

[MIT](LICENSE)
