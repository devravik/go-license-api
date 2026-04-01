# Go License API

A high-performance, multi-tenant license validation service built with Go.

> Designed as a standalone validation microservice to offload heavy license checks from primary applications.

## Core Philosophy

Following the **Spatie-level** standard of clarity and explicitness:
- **Clarity over cleverness**: Code must be understandable in 30 seconds.
- **Explicit over implicit**: No hidden side effects or magic behavior.
- **Small focused components**: Single responsibility per file and package.
- **Predictable structure**: Standardized Go project layout (cmd/internal).
- **Public API stability**: Consistent and reliable integration patterns.

## Architecture & Flow

```text
Request → Tenant Auth → Rate Limit → Queue → Worker → Validation → Response
```

### Pipelines
- **Data Plane (Runtime API)**: Handles high-performance validation requests using in-memory cached data (backed by PostgreSQL). Optimized for low-latency (sub-millisecond under typical conditions).
- **Control Plane (Admin Layer)**: Manages tenants, API keys, and rate limits independently. This separation ensures stability and prevents management tasks from impacting runtime performance.

### Lifecycle
1. **Ingest**: Received via Fiber HTTP API.
2. **Resolve**: Tenant identity and API keys validated against the cache.
3. **Check**: Rate limits enforced via in-memory token bucket.
4. **Buffer**: Request pushed to a buffered channel (Job Queue).
5. **Execute**: Picked up by available workers with request timeout (context) handling.
6. **Respond**: JSON result returned; optimized for low-latency (sub-millisecond under typical conditions).

## Getting Started

### Prerequisites

- Go 1.21 or higher

### Running Locally

```bash
# Install dependencies
go mod tidy

# Start the server
go run main.go
```

The server will be available at `http://localhost:8080`.

## API Documentation

### Validate License

**Endpoint:** `POST /licenses/validate`

**Headers:**
- `X-Tenant-ID`: Identifies the tenant (required in multi-tenant mode).
- `X-API-Key`: Authentication key (required in multi-tenant mode).

**Request Body:**
```json
{
  "key": "ABC-123",
  "product": "v-plugin"
}
```

**Success Response:**
```json
{
  "valid": true,
  "meta": {
    "plan": "enterprise",
    "expires_at": "2025-12-31"
  }
}
```

**Failure Response:**
```json
{
  "valid": false,
  "error": "license_expired"
}
```

## Public API Design

The service is designed with a clean and predictable public API, making it easy to integrate as a package or a standalone service:

```bash
go get github.com/your-username/go-license-api
```

```go
// Example usage as a package
validator := license.NewValidator(cache)

result, err := validator.Validate(ctx, input)
if err != nil {
    // handle error
}
```

## Tech Stack

- **Backend**: Go (Golang) with the Fiber web framework.
- **Concurrency**: Goroutines and buffered channels for the worker pool implementation.
- **Storage**: PostgreSQL (production) using lightweight SQL or query builders.
- **Rate Limiting**: Native Go token bucket implementation for per-tenant control.

## Development Setup

### Prerequisites
- Go 1.21 or higher
- Git

### Initial Setup
```bash
git clone https://github.com/your-username/go-license-api.git
cd go-license-api

go mod tidy
go run main.go
```

## Project Structure

```text
.
├── cmd/
│   └── server/                # Entrypoint (main.go)
│
├── internal/                  # Private application logic
│   ├── app/                   # Use cases (Validation, Tenant, License)
│   ├── domain/                # Business models and rules
│   ├── infrastructure/        # PostgreSQL, Cache, Limiter
│   ├── http/                  # Handlers, Middleware, Routes
│   └── worker/                # Worker pool implementation
│
├── pkg/                       # Reusable public utilities
├── configs/                   # Configuration files
├── migrations/                # Database migrations
├── tests/                     # Unit and integration tests
├── README.md
└── go.mod
```

### Layer Responsibilities
- **Domain**: Pure structs and business rules (e.g., `IsExpired()`). Strictly no DB or HTTP dependencies.
- **Application**: Orchestration layer. Executes use cases and interacts with domain/infrastructure interfaces.
- **Infrastructure**: Implementation of storage (PostgreSQL), caching, and rate limiting.
- **Transport (HTTP)**: Fiber handlers and middleware. Responsible for request parsing and response formatting.

### Interfaces
All infrastructure dependencies must be defined as interfaces in the application layer to ensure high testability and decoupled design.

Example:
```go
type LicenseRepository interface {
    FindByKey(ctx context.Context, key string) (*License, error)
}
```

## Standards

### Naming Conventions
- **Files**: `snake_case` (e.g., `tenant_service.go`, `license_validator.go`).
- **Interfaces**: Explicit naming (e.g., `type TenantRepository interface {}`).
- **Constructors**: Use `New` prefix (e.g., `func NewTenantService(repo TenantRepository)`).

### Code Style
- **Small functions**: Functions should generally be under 50 lines.
- **Dependency Injection**: Use constructor-based DI; avoid globals and hidden side effects.
- **No Shared State**: Avoid shared mutable state without proper synchronization.

### Error Handling
Use typed and structured errors for predictability:
```go
var (
    ErrLicenseExpired = errors.New("license expired")
    ErrInvalidTenant  = errors.New("invalid tenant")
)
```

### Testing Standard
- **Unit (Domain)**: 100% testable logic without dependencies.
- **Integration (Infra)**: Validating database and cache implementations.
- **Mocking**: Used at the Application layer to isolate use cases from infrastructure.

## Configuration

The service utilizes a singleton configuration pattern: **Load once, inject everywhere.**

### Configuration Pattern
1. **Load**: Configuration is loaded from environment variables or files at startup.
2. **Inject**: The `Config` struct is injected into handlers and services via constructors.

### Environment Variables
- `MODE`: Deployment mode (`single` or `multi`).
- `PORT`: Server port (defaults to `8080`).
- `ADMIN_API_KEY`: Secret key required for administrative operations.

### Principles
- **System Settings**: Strictly managed via environment variables (e.g., ports, modes).
- **Business Data**: Tenants, licenses, and limits are managed via PostgreSQL and synced to cache.

## Testing the API

To validate a license using `curl`:
```bash
curl -X POST http://localhost:8080/licenses/validate \
-H "Content-Type: application/json" \
-H "X-Tenant-ID: example_tenant_1" \
-H "X-API-Key: your_api_key" \
-d '{
  "key": "ABC-123",
  "product": "v-plugin"
}'
```

## Admin APIs

The service includes a dedicated control layer for managing tenants and licenses, intentionally isolated from the high-throughput validation pipeline.

### Design Principles
- **Isolation**: Admin APIs are separate from the high-throughput request flow.
- **Reliability**: They are handled outside the worker pool pipeline and excluded from public rate limiting to ensure reliable management access.
- **Purpose**: Designed for integration with internal dashboards, scripts, or automated tooling.

### Security
Administrative access is strictly enforced via the `ADMIN_API_KEY` environment variable. All requests must include the `X-Admin-Key` header for validation.

### Create Tenant
**Endpoint:** `POST /admin/tenants`

**Headers:**
- `X-Admin-Key`: Must match the configured `ADMIN_API_KEY`.

**Request Body:**
```json
{
  "id": "tenant_1",
  "api_key": "abc123",
  "rps": 100,
  "burst": 200
}
```

**Behavior:**
- Persists the tenant in PostgreSQL.
- Updates the in-memory cache immediately.
- Applies dynamic rate-limiting configuration without requiring a restart.

### Alternative Management Approach
For simpler or smaller deployments, tenant management can be handled directly via SQL migrations or internal database tooling. On startup, the service automatically synchronizes the in-memory cache with the current PostgreSQL state, maintaining a production-ready environment without exposing the Admin API.

## Performance & Security

### Performance
- **Zero-DB Validation**: All validation requests are served from an in-memory cache; optimized for low-latency (sub-millisecond under typical conditions).
- **Worker Isolation**: Background processing is decoupled from the request-response cycle via a buffered job queue.
- **Native Efficiency**: Built on Go's high-performance concurrency primitives.

### Security
- **Tenant Isolation**: Strictly enforced at the transport layer via middleware.
- **Admin Protection**: Administrative endpoints are isolated and protected by the `ADMIN_API_KEY`.
- **Data Privacy**: Strict tenant-scoped data access ensures privacy across multi-tenant deployments.


## Future Enhancements
- Distributed queue integration (Kafka / RabbitMQ).
- Performance metrics and monitoring (Prometheus / Grafana).

## Database & Migrations

This project uses versioned SQL migrations to manage database schema evolution.

### Migration Structure
Migrations are stored in the `/migrations/` directory as incremental SQL scripts.

### Core Schema
The following schema defines the foundational tables for tenants and license management:

```sql
CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    api_key TEXT NOT NULL,
    rps INT DEFAULT 100,
    burst INT DEFAULT 200,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE licenses (
    id SERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    key TEXT NOT NULL,
    product_id TEXT,
    product TEXT,
    status TEXT,
    expires_at TIMESTAMP,
    meta JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_license_key ON licenses(key);
CREATE INDEX idx_license_tenant ON licenses(tenant_id);
```

### Running Migrations
Migrations can be applied manually using the standard `psql` interface:
```bash
psql -U postgres -d your_db -f migrations/001_init.sql
```

## Deployment Modes

The service supports both single-tenant and multi-tenant configurations using a unified architecture.

### Mode Selection
Deployment mode is controlled via the `MODE` environment variable:
- `MODE=single`: Optimized for standalone or self-hosted setups.
- `MODE=multi`: Designed for SaaS platforms requiring strict data isolation.

### Single-Tenant Mode
- No tenant-specific headers are required for validation requests.
- All operations are scoped to a default tenant ID (`default`).
- On startup, the system ensures a default tenant is initialized for single-tenant operations.
- Ideal for standalone or self-hosted deployments.

### Multi-Tenant Mode
- Requires `X-Tenant-ID` and `X-API-Key` headers for all requests.
- Enforces strict data and resource isolation between tenants.

### Unified Design Approach
A shared schema is utilized across both modes. The `tenant_id` field is ubiquitous in the data layer, ensuring that a system can scale from a single-tenant instance to a multi-tenant platform without schema modifications or complex migrations.

### Tenant Resolution Lifecycle
1. **Request Intake**: Middleware identifies the current deployment mode.
2. **Contextual Resolution**: If in `single` mode, the context is automatically assigned to the default tenant. If in `multi` mode, the middleware validates the provided headers against the tenant registry.
3. **Scoped Processing**: The storage layer executes queries filtered by the resolved tenant identity.



## Versioning

This project follows semantic versioning (SemVer):
- **MAJOR**: Breaking changes.
- **MINOR**: New features and non-breaking enhancements.
- **PATCH**: Bug fixes and minor maintenance.

## Extensibility

The system is designed for extension via:
- **Custom storage backends** (e.g., SQLite, Redis).
- **Pluggable cache implementations**.
- **Alternative rate limiting strategies**.

All extensions should implement the defined interfaces in the application layer without modifying the core system logic.

## Changelog

Please see [CHANGELOG.md](CHANGELOG.md) for a full release history.

---

## Maintainer

**Ravi K Gupta**

- **Website**: [devravik.github.io](https://devravik.github.io/)
- **Email**: `dev.ravikgupt@gmail.com`
- **LinkedIn**: [linkedin.com/in/ravi-k-dev](https://www.linkedin.com/in/ravi-k-dev)
- **GitHub**: [github.com/devravik](https://github.com/devravik)

---

## License

The MIT License (MIT). Please see [LICENSE](LICENSE) for more information.