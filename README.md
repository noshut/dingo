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
| `hetzner` | in progress |
