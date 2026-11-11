# WRAITH Core

```bash
go test ./...
go build ./cmd/wraith
./wraith -h
./wraith validate [--strict] [file]   # validate NDJSON events (stdin default)
```

Default listen `127.0.0.1:8088`; database `~/.local/share/wraith/wraith.db`.
Config precedence: defaults < `-config file.json` < `WRAITH_*` env < flags.
Only loopback `Host` headers are accepted unless listed in `allowed_hosts`.

API: see `internal/api/api.go`; all routes under `/api/v1`. Lists accept
`limit` (1–500, default 50) and `offset`, and return
`{"data": [...], "pagination": {"limit","offset","total"}}`. Errors return
`{"error": {"code","message"}}`.
