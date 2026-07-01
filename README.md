# ntpskew

A small Go server that measures clock skew against NTP servers on its own
schedule and exposes the results as Prometheus metrics.

## Why its own schedule, not scrape-driven

The poller runs independently of Prometheus scrapes. On each tick it queries
every configured NTP server and updates gauges; `/metrics` just returns
whatever was last measured. This avoids doing a network round-trip inside
the scrape path (which can be slow or time out) and avoids hammering the NTP
server if something scrapes you often.

## Build & run

```bash
go build -o ntpskew .
./ntpskew
```

## Configuration

Every option has a flag and an equivalent env var (flag wins if both are set):

| Flag              | Env var             | Default        | Description                              |
|-------------------|----------------------|----------------|-------------------------------------------|
| `-ntp-servers`    | `NTP_SERVERS`        | `pool.ntp.org` | Comma-separated list of NTP servers       |
| `-poll-interval`  | `NTP_POLL_INTERVAL`  | `30s`          | How often each server is queried          |
| `-query-timeout`  | `NTP_QUERY_TIMEOUT`  | `5s`           | Per-query timeout                         |
| `-listen-addr`    | `LISTEN_ADDR`        | `:9917`        | Metrics HTTP server address               |
| `-metrics-path`   | `METRICS_PATH`       | `/metrics`     | Path to serve metrics on                  |

Example with multiple servers on a faster poll:

```bash
./ntpskew -ntp-servers="time.cloudflare.com,time.google.com,pool.ntp.org" -poll-interval=15s
```

## Metrics exposed

- `ntp_clock_offset_seconds{server}` — local clock minus server clock. Positive = local clock is ahead. This is the main "skew" measurement.
- `ntp_round_trip_seconds{server}` — RTT of the last query.
- `ntp_stratum{server}` — stratum reported by the server.
- `ntp_root_dispersion_seconds{server}` — server's reported root dispersion.
- `ntp_last_query_timestamp_seconds{server}` — unix time of the last attempt (success or failure).
- `ntp_last_success_timestamp_seconds{server}` — unix time of the last successful query.
- `ntp_queries_total{server,outcome}` — counter, `outcome` is `success` or `error`.

Each server polls independently and concurrently, so one slow/unreachable
server won't hold up metrics for the others. A `/healthz` endpoint returns
`200 ok` for basic liveness checks.

## Prometheus scrape config

```yaml
scrape_configs:
  - job_name: ntpskew
    static_configs:
      - targets: ["localhost:9917"]
```

Once scraped, `ntp_clock_offset_seconds` is what you'd graph to see drift
over time — e.g. `rate(ntp_clock_offset_seconds[1h])` gives you an estimate
of the clock's drift rate.

## Note on go.mod

This was built in a sandboxed environment where the module proxy
(`proxy.golang.org`) and `golang.org` itself were not reachable, so `go.mod`
uses `replace` directives pointing transitive `golang.org/x/*` and
`google.golang.org/protobuf` dependencies at their GitHub mirrors. These are
functionally identical to the originals. If you have normal network access
you can safely delete the `replace` lines and run `go mod tidy` to pull the
canonical modules instead — nothing else needs to change.
