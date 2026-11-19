# dingo

DynDNS client written in Go – keeps A/AAAA records in sync with your public IP, also for DNS providers without native DynDNS support (e.g. Hetzner).

## Quick start (Docker)

```bash
cp config.example.yaml config.yaml   # edit zones/records
export HETZNER_TOKEN=...
docker compose up -d
```

## Supported providers

| Type      | Status      |
|-----------|-------------|
| `hetzner` | ✅ Hetzner Cloud API (Console DNS), token with read & write |

> dingo owns every record it manages: on update, the whole RRSet (name + type) is replaced with the current IP. Don't configure names that hold additional, manually managed A/AAAA values.
