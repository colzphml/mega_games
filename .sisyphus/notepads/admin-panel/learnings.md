# Learnings (admin-panel)

## 2026-02-01 Task: discovery-status-tables
- Postgres status tables are per-service, with a pair of tables: `<name>_status` and `<name>_failed`.
- Common status values: `new`, `processed`.
- Processor: `discord_message_status`, `discord_message_failed` (see `discord_kafka_processor/internal/store/store.go`).
- Week formatter: `week_message_status`, `week_message_failed` (see `discord_kafka_week_formatter/internal/store/store.go`).
- Telegram week sender: `telegram_week_status`, `telegram_week_failed` (see `discord_kafka_telegram_week_sender/internal/store/store.go`).
- Telegram game sender: `telegram_game_status`, `telegram_game_failed` (see `discord_kafka_telegram_game_sender/internal/store/store.go`).
- Game image: `game_image_status`, `game_image_failed` (see `discord_kafka_game_image/internal/pgstore/store.go`).

## 2026-02-01 Task: discovery-config-patterns
- Canonical env/config pattern: `Load()` in `*/internal/config/config.go` with helper functions for required/optional env and defaults.
- Postgres DSN pattern: `PostgresDSN()` method on Config (example: `discord_kafka_week_formatter/internal/config/config.go`).
- HTTP/health + graceful shutdown pattern: `signal.NotifyContext` + `http.Server` + `Shutdown` (example: `discord_kafka_listener/cmd/discord-kafka-listener/main.go`).

# Learnings from existing services (admin-panel task)

- Pattern: env/config parsing
  - Example sources: 
    - /Users/colz/gitrepos/envs/mega_games/discord_kafka_listener/internal/config/config.go
  - Takeaway: A Load() function builds a Config by reading environment variables via small helpers: requiredEnv, optionalEnv, durationEnv, intEnv, etc. HealthAddr is loaded via optionalEnv with a default (defaultHealthAddr). This keeps the config immutable once loaded and provides clear defaults.

- Pattern: Postgres DSN creation
  - Example sources:
    - /Users/colz/gitrepos/envs/mega_games/discord_kafka_week_formatter/internal/config/config.go
  - Takeaway: Config has a PostgresDSN() string method that formats a DSN like:
      postgres://<user>:<pass>@<host>:<port>/<db>?sslmode=<mode>
    This centralizes DSN creation and ensures TLS/SSL mode is consistently applied from POSTGRES_SSLMODE env.

- Pattern: Health HTTP server and graceful shutdown
  - Example sources:
    - /Users/colz/gitrepos/envs/mega_games/discord_kafka_listener/cmd/discord-kafka-listener/main.go
  - Takeaway: Use signal.NotifyContext to derive a cancellation context. Expose a health HTTP endpoint (e.g., /health) that checks dependencies and returns 200 when ready. Run HTTP server with cfg.HealthAddr as address, and shutdown gracefully on context cancellation with Server.Shutdown(ctx).

-.env naming recommendation for admin_panel (align with patterns above):
  - HEALTH_ADDR: :8080 (or :{port})
  - TZ or TIMEZONE: (e.g., TZ or TIMEZONE depending on project convention)
  - POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB
  - POSTGRES_SSLMODE (default: disable)
  - POSTGRES_CONNECT_TIMEOUT, POSTGRES_CONNECT_RETRY_DELAY, POSTGRES_CONNECT_MAX_ATTEMPTS (for resilient startup)

- Graceful shutdown pattern to replicate:
  - Use signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
  - Run services (http server, workers) in goroutines, and call server.Shutdown or worker cancel on ctx.Done()
  - Coordinate via WaitGroup and ensure all goroutines exit cleanly before exiting main.

## Grafana Embedding & Loki API Research

### Grafana Iframe Embedding

#### Required Settings
- Set `allow_embedding = true` in Grafana configuration (grafana.ini)
- Configure X-Frame-Options headers properly
- Default anonymous access may need to be enabled for public dashboards

#### URL Patterns for localhost:3000
**Dashboard with variables:**
```
http://localhost:3000/d/some-dashboard-id?var-service=my-service&var-time-range=1h
```

**Embedded iframe URL:**
```html
<iframe 
  src="http://localhost:3000/d/some-dashboard-id?var-service=my-service&var-time-range=1h&theme=light&kiosk"
  width="100%" 
  height="600"
  frameborder="0">
</iframe>
```

**Panel-only embedding:**
```
http://localhost:3000/d-solo/some-dashboard-id/some-panel-id?var-service=my-service
```

#### Key Parameters
- `var-<name>`: Template variables
- `theme=light/dark`: Force theme
- `kiosk`: Remove top navigation
- `orgId=<id>`: For multi-organization setups
- `from=<timestamp>&to=<timestamp>`: Time range (Unix timestamp or relative)

#### Security Considerations
- X-Frame-Options: DENY by default, need `allow_embedding = true`
- Same-origin issues: Admin panel (8081) vs Grafana (3000) are different origins
- CORS headers must be properly configured
- For production: Use reverse proxy with authentication headers instead of anonymous access

### Loki HTTP API

#### Query Range Endpoint (localhost:3100)
**Base URL:**
```
http://localhost:3100/loki/api/v1/query_range
```

**Parameters:**
- `query`: LogQL query string
- `start`: Start time (RFC3339 or Unix nanoseconds)
- `end`: End time (RFC3339 or Unix nanoseconds)  
- `limit`: Maximum number of entries (default: 100)
- `step`: Query resolution step (optional)
- `direction`: forward/backward (default: backward)

#### Example Requests

**Query last hour logs:**
```bash
curl "http://localhost:3100/loki/api/v1/query_range" \
  -G -s \
  --data-urlencode "query={job=\"docker\"}" \
  --data-urlencode "start=$(date -u -d '1 hour ago' --rfc-3339)" \
  --data-urlencode "end=$(date -u --rfc-3339)" \
  --data-urlencode "limit=1000"
```

**Query with service filter:**
```bash
curl "http://localhost:3100/loki/api/v1/query_range" \
  -G -s \
  --data-urlencode 'query={job="docker", service="discord_kafka_listener"}' \
  --data-urlencode "start=2026-02-01T10:00:00Z" \
  --data-urlencode "end=2026-02-01T11:00:00Z" \
  --data-urlencode "limit=500"
```

#### Go Integration Example
```go
import (
    "context"
    "fmt"
    "net/http"
    "time"
)

type LokiResponse struct {
    Status string `json:"status"`
    Data   struct {
        ResultType string `json:"resultType"`
        Result    []struct {
            Stream map[string]string `json:"stream"`
            Values  [][]string       `json:"values"`
        } `json:"result"`
    } `json:"data"`
}

func QueryLoki(ctx context.Context, query string, start, end time.Time) (*LokiResponse, error) {
    url := "http://localhost:3100/loki/api/v1/query_range"
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    q := req.URL.Query()
    q.Set("query", query)
    q.Set("start", start.Format(time.RFC3339Nano))
    q.Set("end", end.Format(time.RFC3339Nano))
    q.Set("limit", "1000")
    req.URL.RawQuery = q.Encode()
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var result LokiResponse
    err = json.NewDecoder(resp.Body).Decode(&result)
    return &result, err
}
```

### Production Recommendations

#### For Admin Panel Integration
1. **Authentication**: Use API tokens instead of anonymous access
2. **Reverse Proxy**: Put Grafana behind nginx with auth headers
3. **Rate Limiting**: Implement rate limiting for both Grafana and Loki APIs
4. **Error Handling**: Graceful degradation when services are unavailable

#### Recommended Architecture
```
Admin Panel (8081)
├── Grafana iframe (localhost:3000/d/...)
│   └── Use org-specific API tokens
└── Direct Loki API calls (localhost:3100/loki/api/v1/)
    └── Cache frequent queries (Redis/Go cache)

Security:
├── CSP headers to restrict iframe sources
├── Reverse proxy for统一 authentication
└── Network policies to limit cross-service access
```

#### Common Issues & Solutions
- **X-Frame-Options denied**: Set `allow_embedding = true` in grafana.ini
- **CORS errors**: Configure `allow_origin = *` or specific origins
- **Authentication loops**: Use JWT tokens passed as query parameters
- **Performance**: Use `query_range` with appropriate time windows, avoid instant queries
- **Rate limiting**: Implement exponential backoff for failed requests

### URL Examples Summary

**Grafana Dashboard (iframe):**
```
http://localhost:3000/d/dashboard-id?var-service=my-service&theme=light&kiosk
```

**Grafana Panel (solo):**
```
http://localhost:3000/d-solo/dashboard-id/panel-id?var-service=my-service
```

**Loki Query Range:**
```
http://localhost:3100/loki/api/v1/query_range?query={job="docker"}&start=2026-02-01T10:00:00Z&end=2026-02-01T11:00:00Z&limit=1000
```

## 2026-02-01 Task: admin-scaffolding
- **Root Module Dependencies**: The root `go.mod` did not include `pgx/v5` even though sub-modules might use it. When adding a new binary to `cmd/` in the root module, we needed to explicitly `go get github.com/jackc/pgx/v5` to satisfy dependencies for `cmd/admin_panel`.
