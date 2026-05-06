# trafficloadsim

A command-line HTTP load tester written in Go. Send traffic to any API endpoint and control how that traffic ramps up or down over time to simulate real-world patterns.

## Install

```sh
go install trafficloadsim@latest
```

Or build from source:

```sh
git clone https://github.com/yourorg/trafficloadsim
cd trafficloadsim
go build -o trafficloadsim .
```

## Quick start

```sh
# Ramp from 10 to 500 req/s over 60 seconds
trafficloadsim -url https://api.example.com/endpoint \
      -min-rps 10 \
      -max-rps 500 \
      -duration 60s \
      -profile linear
```

## Flags

| Flag | Default | Description |
|---|---|---|
| `-url` | *(required)* | Target URL |
| `-method` | `GET` | HTTP method |
| `-header` | | `Key: Value` header, repeatable |
| `-body` | | Request body string |
| `-profile` | `linear` | Traffic profile (see below) |
| `-min-rps` | `1` | Starting requests per second |
| `-max-rps` | `100` | Peak requests per second |
| `-duration` | `30s` | How long to run |
| `-concurrency` | `50` | Max in-flight requests |
| `-steps` | `5` | Step count for the `step` profile |
| `-timeout` | `10s` | Per-request timeout |
| `-insecure` | `false` | Skip TLS certificate verification |
| `-follow-redirects` | `true` | Follow HTTP redirects |
| `-output` | `text` | Output format: `text` or `json` |

## Traffic profiles

### `constant`
Fixed rate at `-max-rps` for the entire duration.

```
rps
 │  ████████████████████████
 │
 └──────────────────────────▶ time
```

### `linear` (default)
Smooth ramp from `-min-rps` up to `-max-rps` over the full duration.

```
rps
 │                    ██████
 │              ██████
 │        ██████
 │  ██████
 └──────────────────────────▶ time
```

### `step`
Staircase increase from `-min-rps` to `-max-rps` in `-steps` equal jumps.

```
rps
 │                 █████████
 │           █████
 │     █████
 │  ██
 └──────────────────────────▶ time
```

### `wave`
Sinusoidal oscillation between `-min-rps` and `-max-rps`. Useful for testing how a service handles cyclic load.

```
rps
 │         ████
 │      ███    ███
 │  ████          ████
 │                    ██████
 └──────────────────────────▶ time
```

### `spike`
Constant low traffic with a sudden burst to `-max-rps` in the middle 20% of the run.

```
rps
 │            ██████
 │            █    █
 │  ██████████      ██████████
 └──────────────────────────▶ time
```

## Examples

**POST with JSON body and auth header:**
```sh
trafficloadsim -url https://api.example.com/users \
      -method POST \
      -header "Authorization: Bearer $TOKEN" \
      -header "Content-Type: application/json" \
      -body '{"name":"test"}' \
      -max-rps 200 \
      -duration 30s
```

**Step profile to find the breaking point:**
```sh
trafficloadsim -url https://api.example.com/search \
      -profile step \
      -min-rps 10 \
      -max-rps 1000 \
      -steps 10 \
      -duration 60s \
      -concurrency 200
```

**Spike test:**
```sh
trafficloadsim -url https://api.example.com/checkout \
      -profile spike \
      -min-rps 5 \
      -max-rps 300 \
      -duration 60s
```

**JSON output for scripting:**
```sh
trafficloadsim -url https://api.example.com/health \
      -max-rps 50 \
      -duration 10s \
      -output json | jq '.results.avg_rps, .latency.p99_ms'
```

## Output

The live display updates every second on stderr:

```
  [████████████░░░░░░░░]   24s/60s   target    240 rps   actual  238.4 rps   total 4,521   err 0.0%   mean   12.3ms
```

The final text report prints to stdout:

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  Load Test Results
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Target       GET https://api.example.com/endpoint
  Profile      linear 10 → 500 rps over 1m0s
  Duration     1m0s (actual 60.0s)
  Concurrency  50

  Requests
    Total       15,330
    Success     15,298 (99.8%)
    Errors      32 (0.2%)
    Throughput  255.5 req/s avg
    Received    18.2 MB

  Latency
    Min    2.10ms
    Mean   14.32ms
    P50    11.45ms
    P90    28.71ms
    P95    41.20ms
    P99    89.54ms
    Max    1.23s

  Status Codes
    200    15,298
    503    32
```

The JSON output (stdout only, live display stays on stderr) is pipeline-friendly:

```json
{
  "config": { "url": "...", "profile": "linear", "min_rps": 10, "max_rps": 500, ... },
  "results": { "total": 15330, "success": 15298, "avg_rps": 255.5, ... },
  "latency": { "p50_ms": 11.45, "p90_ms": 28.71, "p99_ms": 89.54, ... },
  "status_codes": { "200": 15298, "503": 32 },
  "errors": {}
}
```

## How it works

- A **rate controller** goroutine fires every 10 ms and computes how many tokens to emit based on `∫₀ᵗ rate(τ) dτ` (the analytic integral of the traffic profile), not the instantaneous rate. This keeps the total request count accurate even for non-constant profiles.
- A **worker pool** of `-concurrency` goroutines pulls tokens and issues HTTP requests concurrently. Excess tokens are dropped (not queued) when workers are saturated, preventing an artificial backlog.
- A **single collector goroutine** receives results over a channel with no lock contention in the hot path. Latency percentiles are computed once at the end by sorting all samples.

## Development

```sh
go test ./...               # run all tests
go test ./... -race         # with race detector
go test ./... -v            # verbose output
go test ./internal/traffic/ # single package
```

## License

GPL v3
