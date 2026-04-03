# Loadtest CLI Reference

## Overview
The loadtest CLI seeds test data (control plane) and simulates runtime traffic (data plane). It supports HTTP (full pipeline) and direct (service layer) modes and reports latency percentiles and throughput.

## Commands

### Seed
Prepare tenants, products, and licenses (DB write-through + cache update).

```bash
cli loadtest seed \
  --tenants <N> \
  --products <M> \
  --licenses <K>
```

Flags:
- --tenants: number of tenants to create (default 10)
- --products: products per tenant (default 3)
- --licenses: licenses per tenant (default 1000)

Notes:
- The command performs only pre-DB validations and then writes; no data-plane paths are touched.
- For large runs (e.g., 50 tenants × 3 products × 5000 licenses = 250k licenses), increase the CLI timeout:
  - Example: `cli --timeout=10m loadtest seed --tenants=50 --products=3 --licenses=5000`

Examples:
```bash
cli loadtest seed --tenants=2 --products=1 --licenses=10
cli --timeout=10m loadtest seed --tenants=50 --products=3 --licenses=5000
```

### Run
Simulate concurrent runtime operations with configurable rates and mixes.

```bash
cli loadtest run \
  --tenants <N> \
  --products <M> \
  --licenses <K> \
  --workers <W> \
  --duration <D> \
  --rps <R> \
  --mode <http|direct> \
  [--base-url <URL>] \
  [--burst <B>] \
  [--low-rps-tenants <L>] \
  [--cold-start] \
  [--skew <hot:cold>] \
  [--invalid-rate <0..1>]
```

Flags:
- --workers: concurrent workers (goroutines)
- --duration: total test duration (e.g., 60s)
- --rps: target global requests/sec
- --mode: http (full pipeline) or direct (service layer)
- --base-url: base URL for HTTP mode (default http://localhost:8080)
- --burst: additional token bucket burst capacity
- --low-rps-tenants: number of tenants to stress with low RPS limits
- --cold-start: do not prewarm caches before running
- --skew: traffic skew hot:cold (default 80:20), e.g., 80% of traffic to 20% tenants
- --invalid-rate: fraction of invalid requests (default 0.10)

Operation mix (fixed):
- validate: 70%
- activate: 20%
- usage: 10%

Examples:
```bash
cli loadtest run \
  --tenants=2 --products=1 --licenses=10 \
  --workers=50 --duration=30s --rps=1000 \
  --mode=http --base-url=http://localhost:8080 \
  --skew=80:20 --invalid-rate=0.10

cli loadtest run \
  --tenants=10 --products=2 --licenses=1000 \
  --workers=200 --duration=60s --rps=5000 \
  --mode=direct --cold-start --burst=10000
```

## Output
At the end of a run, a JSON summary is printed:
```json
{
  "requests": 300000,
  "success": 298200,
  "failures": 1800,
  "avg_ms": 3.2,
  "p95_ms": 7.8,
  "p99_ms": 15.4,
  "throughput": 5000,
  "errors": {
    "rate_limited": 1000,
    "invalid": 600,
    "not_found": 200
  },
  "by_op": {
    "validate": 210000,
    "activate": 60000,
    "usage": 30000
  }
}
```

## Tips
- For large seeds, use a longer timeout (e.g., `--timeout=10m`).
- To stress rate-limiting, use `--low-rps-tenants` and high `--rps`.
- Use `--mode=http` to validate the full pipeline (auth, rate limit, queue, workers).
- Use `--mode=direct` for maximum raw service throughput without HTTP overhead.

