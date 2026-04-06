# Setup & Quick Start Guide

This guide will help you get the Go License API up and running on your local machine.

## Prerequisites

- **Go**: Version 1.21 or higher. [Download Go](https://go.dev/dl/)
- **Git**: For cloning the repository.
- **PostgreSQL**: Required for control plane persistence. Data-plane validation never queries DB.

## Installation

1.  **Clone the Repository**:
    ```bash
    git clone https://github.com/devravik/go-license-api.git
    cd go-license-api
    ```

2.  **Install dependencies**:
    ```bash
    go mod tidy
    ```

3.  **Prepare environment variables**:
    Create a `.env` file in the root directory. Minimal example:
    ```env
    ADMIN_API_KEY=your-secret-admin-key
    PORT=8080
    ```
    Common operational settings (optional; see `internal/setup/config.go`):
    ```env
    APP_NAME="Go License API"
    APP_ENV=development
    JSON_ENGINE=std
    SIGNING_KEY_PATH=""
    WORKER_COUNT=8
    WORKER_QUEUE_SIZE=5000
    WORKER_TIMEOUT=1500ms
    VALIDATION_TIMEOUT=2s
    CLIENT_TIMEOUT=3s
    MIN_LICENSE_KEY_LEN=8
    AUDIT_WORKER_COUNT=2
    AUDIT_QUEUE_SIZE=8000
    AUDIT_RETRY_COUNT=1
    AUDIT_RETRY_DELAY=50ms
    ADMIN_ALLOWED_CIDRS=
    WEBHOOK_ENCRYPTION_KEY=
    SHUTDOWN_TIMEOUT=30
    AUDIT_FLUSH_TIMEOUT=5
    WORKER_DRAIN_TIMEOUT=0
    ```

## Configuration

Configuration is centralized in `internal/setup/config.go`. The server requires `ADMIN_API_KEY` and prefers standard `PORT`.

### General Settings
- `APP_NAME`: Name of the application.
- `PORT` (or `APP_PORT` fallback): Port on which the server will run (default: `3000`).
- `ADMIN_API_KEY`: Secret key required for administrative operations via the `X-Admin-Key` header.

### Timeouts & Workers
- `WORKER_COUNT`, `WORKER_QUEUE_SIZE`, `WORKER_TIMEOUT` (bounded worker pool)
- `VALIDATION_TIMEOUT` (must be ≤ `CLIENT_TIMEOUT`)
- `CLIENT_TIMEOUT`

## Running the Server

Start the application from the root directory:

```bash
go run cmd/server/main.go
```

You should see the Fiber banner confirm the server has started:

```text
    _______ __
   / ____(_) /_  ___  _____
  / /_  / / __ \/ _ \/ ___/
 / __/ / / /_/ /  __/ /
/_/   /_/_.___/\___/_/          v3.1.0
--------------------------------------------------
INFO Server started on:         http://127.0.0.1:8080
INFO Application name:          Go License API
```

## Quick Start (Testing)

Perform a quick health check to verify the API is responsive:

```bash
curl http://localhost:8080/health
```

**Expected JSON Response**:
```json
{"status":"up"}
```

### Validating a License

Provide the API key via header `X-API-Key`:

```bash
curl -X POST http://localhost:8080/licenses/validate \
-H "Content-Type: application/json" \
-H "X-API-Key: example_api_key" \
-d '{
  "license_key": "ABC-123-XYZ",
  "product_code": "v-plugin"
}'
```

### Signed License and JWKS
- `GET /licenses/{license_key}/signed` (and legacy `{key}`) returns a JWS/JWT when signing is configured.
- `POST /licenses/signed` accepts a JSON body with `license_key` and returns a JWS/JWT.
- `GET /.well-known/jwks.json` exposes public keys for verification when signing is enabled.
