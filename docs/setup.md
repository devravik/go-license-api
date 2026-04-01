# Setup & Quick Start Guide

This guide will help you get the Go License API up and running on your local machine.

## Prerequisites

- **Go**: Version 1.21 or higher. [Download Go](https://go.dev/dl/)
- **Git**: For cloning the repository.
- **PostgreSQL**: Required for production; local development can use the in-memory cache but needs a DB connection if persistency is required.

## Installation

1.  **Clone the Repository**:
    ```bash
    git clone https://github.com/devravik/go-license-api.git
    cd go-license-api
    ```

2.  **Install Dependencies**:
    ```bash
    go mod tidy
    ```

3.  **Prepare Environment Variables**:
    Create a `.env` file in the root directory. You can use the following standard values:
    ```env
    APP_NAME="Go License API"
    APP_PORT=8080
    APP_MODE=multi
    ADMIN_API_KEY=your-secret-admin-key
    LOG_ENABLED=true
    LOG_OUTPUT=stdout
    LOG_DIR=./logs
    ```

## Configuration

The application centralizes configuration in the `configs/` package, making it easy to manage through environment variables.

### General Settings
- `APP_NAME`: Name of the application.
- `APP_PORT`: Port on which the server will run (default: `8080`).
- `APP_MODE`: `single` for single-tenant or `multi` for multi-tenant deployment.
- `ADMIN_API_KEY`: Secret key required for administrative operations via the `X-Admin-Key` header.

### Logging Settings
The service includes a production-ready logging system with automatic rotation:
- `LOG_ENABLED`: Set to `true` to enable logging (default: `true`).
- `LOG_OUTPUT`: `stdout` for terminal output or `file` for file-based logging (default: `stdout`).
- `LOG_DIR`: Directory where log files are stored (default: `./logs`).
- `LOG_LEVEL`: Logging verbosity (default: `error`).

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

In `multi` tenant mode, you must provide the tenant identity and API key:

```bash
curl -X POST http://localhost:8080/licenses/validate \
-H "Content-Type: application/json" \
-H "X-Tenant-ID: example_tenant" \
-H "X-API-Key: example_api_key" \
-d '{
  "key": "ABC-123-XYZ",
  "product": "v-plugin"
}'
```
