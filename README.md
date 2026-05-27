# pulse

A small concurrent HTTP health checker, usable as a CLI or as a service.
Standard library only - no third-party dependencies.

```
cmd/pulse          entrypoint, subcommand + flag parsing, signal handling
internal/check     prober interface, HTTP prober, generic bounded worker pool
internal/server    JSON API, logging middleware, graceful shutdown
```

## CLI

```sh
go run ./cmd/pulse check https://go.dev https://example.com
go run ./cmd/pulse check -c 16 -timeout 3s   # reads targets from stdin
```

Exits non-zero if any target is unhealthy, so it drops into CI or a cron job.

## Service

```sh
go run ./cmd/pulse serve -addr :8080
curl -s localhost:8080/check -d '{"targets":["https://go.dev"]}' | jq
```

`POST /check` probes the given targets concurrently and returns one JSON object
per target. `GET /healthz` is a liveness probe. The server drains in-flight
requests on SIGINT before exiting.

## Test

```sh
go test ./...
go test -race ./...
```

## Design notes

- `RunPool[In, Out]` is a generic, order-preserving worker pool. Concurrency is
  bounded by the number of workers; each worker writes to its own output index,
  so results need no locking.
- `Prober` is an interface, so the HTTP implementation is swapped for a stub in
  tests - no network is touched and the suite stays fast and deterministic.
- Cancellation is wired end to end: an HTTP client disconnect or a Ctrl+C
  propagates through `context.Context` down to the in-flight probes.
- Probe failures are wrapped errors; `ErrUnhealthy` is a sentinel so callers can
  distinguish a bad status code from a transport error with `errors.Is`.
