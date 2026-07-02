# Architecture

## Overview

torrent-search-go is a stateless Go HTTP service that aggregates torrent metadata from multiple public indexers, resolves premium streaming links via Real-Debrid, manages user sessions and favorites in MongoDB, and exposes a clean REST API consumed by web or mobile frontends.

The design priorities are:
- **Concurrency** — scraping N indexers in parallel per search request using goroutines
- **Low latency** — in-process caching and aggressive use of Go's standard library HTTP stack
- **Operational simplicity** — single binary, no runtime dependencies beyond MongoDB

## High-Level Component Map

```
                    ┌─────────────────────────────────────────────┐
                    │                  torrent-search-go           │
                    │                                              │
  HTTP client ─────►│  Router (stdlib ServeMux + custom middleware) │
                    │          │                                   │
                    │    ┌─────▼──────────────────────────────┐   │
                    │    │           Handler layer             │   │
                    │    │  auth · torrents · cache · images   │   │
                    │    │  favorites · proxy · monitoring    │   │
                    │    │  monitoring · health                │   │
                    │    └─────┬──────────────────────────────┘   │
                    │          │                                   │
                    │    ┌─────▼──────────────────────────────┐   │
                    │    │          Service layer              │   │
                    │    │  scraper.Service · jobs.Runner      │   │
                    │    │  realdebrid.Client                  │   │
                    │    └─────┬──────────────────────────────┘   │
                    │          │                                   │
                    │    ┌─────▼──────────────────────────────┐   │
                    │    │      Storage abstraction            │   │
                    │    │  pkg/storage.Database interface     │   │
                    │    │  ──── implemented by pkg/mongo ──── │   │
                    │    └─────┬──────────────────────────────┘   │
                    └──────────┼──────────────────────────────────┘
                               │
              ┌────────────────┼────────────────────────┐
              │                │                        │
         ┌────▼────┐    ┌──────▼──────┐     ┌──────────▼──────────┐
         │ MongoDB │    │ Real-Debrid │     │  External scrapers  │
         │  cloud  │    │    API      │     │ (piratebay, yts …)  │
         └─────────┘    └─────────────┘     └─────────────────────┘
```

## Request Lifecycle

```
1.  Incoming HTTP request
       │
2.  Global middleware chain (ordered)
       ├─ Recovery (panic → 500)
       ├─ CORS headers
       ├─ Request logger + API usage recorder
       └─ Trust-proxy (production: unwrap X-Forwarded-For)
       │
3.  Route dispatch  (Go 1.22 ServeMux — "METHOD /path/{param}")
       │
4.  Route-level middleware (per-route, applied before handler)
       ├─ RequireAuth  — validates session cookie / Bearer token
       ├─ OptionalAuth — attaches user if present, continues anyway
       ├─ StaticTokenAuth — API-key gate for addon/cache KV endpoints
       ├─ IPAllowlist  — restricts /api/monitoring/* by IP/CIDR
       └─ DashboardAuth — password gate for monitoring dashboard
       │
5.  Handler (reads/writes JSON, delegates to services)
       │
6.  Response written
       └─ ResponseWriter wrapper captures status code for logger
```

## Concurrency and Background Jobs Model

### Per-Request Concurrency

Torrent search is I/O-bound: each indexer requires at least one HTTP round trip. `scraper.Service.SearchAll` fans out one goroutine per registered scraper, collects results on a buffered channel, then merges and returns — all within a single request context. If a scraper times out (30-second HTTP client timeout) its partial error is silently swallowed so one slow site never blocks the response.

### Background Job Scheduler

`handlers.JobScheduler` owns a `time.Ticker`-based loop for each job. The scheduler starts after route registration and stops cleanly when the OS sends `SIGINT`/`SIGTERM`:

```
main() starts graceful shutdown
  │
  ├─ jobScheduler.Stop(done)   ← drains running job goroutines
  └─ server.Shutdown(ctx)       ← drains HTTP connections (30 s timeout)
```

| Job | Default interval | What it does |
|-----|-----------------|--------------|
| Storage cleanup | 1 hour | Deletes expired sessions, exchange codes, and KV cache rows |
| Stream URL refresh | 24 hours | Re-resolves Real-Debrid links for all user favorites |
| Description/image cache | 6 hours | Pre-fetches cover images for favorited torrents |
| Search results cache | 6 hours | Pre-warms search results for common queries |
| Job log maintenance | 24 hours | Rotates / compresses background-job log files |

Each job writes structured logs to `LOG_DIR/` and exposes its status via `/api/monitoring/*` endpoints.

## Why Go

- **Single binary deployment** — `go build` produces one self-contained binary; no interpreter, no package manager at runtime.
- **Goroutine-per-request concurrency** — parallel scraping of 7 indexers is natural; no event-loop callback hell.
- **Memory efficiency** — idle RSS < 50 MB; no JVM/V8 overhead.
- **Standard library HTTP** — `net/http` ServeMux (Go 1.22+) supports method-and-path patterns natively; zero framework overhead.
- **Type safety** — structs for every model and config; mismatches caught at compile time rather than runtime.
- **Fast cold start** — the binary starts in < 100 ms, making it friendly to container restarts and rolling deploys.
