# Fluxa Security

This document describes the security model for Fluxa's local Docker Compose platform.

---

## Overview

Fluxa is a **local-only fraud detection platform** designed to run on a developer's machine via Docker Compose. It is not intended for production deployment without the hardening steps described in the [Production Hardening](#production-hardening) section below.

---

## Local Development Credentials

All services use fixed default credentials suitable for local development only. These are intentionally simple — the platform has no external network exposure.

| Service | Credential | Value | Where set |
|---------|-----------|-------|-----------|
| PostgreSQL | User / Password | `fluxa_user` / `fluxa_password` | `docker-compose.yml`, service env vars |
| RabbitMQ | User / Password | `fluxa` / `fluxa_pass` | `docker-compose.yml`, service env vars |
| MinIO | Access Key / Secret | `minioadmin` / `minioadmin123` | `docker-compose.yml`, service env vars |
| Grafana | Admin password | `admin` | `docker-compose.yml` |
| PostgreSQL (Grafana datasource) | Password | `fluxa_password` | `deploy/grafana/provisioning/datasources/prometheus.yml` |

**Why this is acceptable for local use:**
- All ports are bound to `localhost` only — nothing is exposed to external networks
- The platform is explicitly scoped to single-developer machines (no shared infra)
- Credentials are consistent across services intentionally so the stack works with `make up` and nothing else
- No real user data or production data is processed

**What is NOT acceptable:**
- Do not deploy this stack to any networked server with these credentials
- Do not use these credentials as a template for production configs
- Do not commit a `.env` file with production secrets (`.env` is gitignored)

---

## What's Protected

### SQL Injection
All database queries use parameterized statements — no string concatenation in SQL.

```go
// internal/db/db.go — parameterized throughout
db.QueryRowContext(ctx, "SELECT ... WHERE user_id = $1", userID)
```

### Payload Integrity
SHA-256 hash is computed at ingest and verified at the processor before persisting. A mismatch triggers a non-retryable discard — the message is ACKed and dropped, never retried with corrupted data.

- Hash computed: `services/ingest/main.go`
- Hash verified: `services/processor/main.go`

### Exactly-Once Processing
Idempotency is enforced via `SELECT FOR UPDATE` on `idempotency_keys` followed by `ON CONFLICT DO NOTHING` on `events`. Duplicate messages from RabbitMQ redelivery cannot result in duplicate DB rows.

- Implementation: `internal/idempotency/idempotency.go`

### Poison Message Isolation
Malformed or schema-invalid messages are classified as `NonRetryableError` and ACKed immediately — they cannot trigger retry storms or block the queue.

- Error types: `internal/domain/errors.go`
- Classification: `services/processor/main.go`

### Secrets Never Logged
Structured logging (`internal/logging/logger.go`) only logs correlation IDs, event IDs, service names, and latency. Passwords, tokens, and payload contents are never included in log output.

### Large Payload Offload
Event payloads exceeding 256 KB are stored in MinIO (local S3-compatible store) and referenced by key in RabbitMQ. This prevents oversized messages from reaching the broker.

- Threshold: `internal/adapters/minio.go`

### Connection Limits
PostgreSQL connections are pooled and capped at 10 per service (`internal/db/db.go`). This prevents connection exhaustion under load.

---

## .gitignore Coverage

The following sensitive patterns are gitignored:

```
.env
.env.local     # environment variable overrides
.claude/       # Claude Code session files
data/          # Kaggle CSV datasets (large, contains PII-adjacent data)
out/           # build artifacts and deployment outputs
/replay        # compiled service binaries
/ingest
/processor
/query
bootstrap
```

No secrets, datasets, or build artifacts should appear in `git status` on a clean checkout.

---

## Production Hardening

If this platform were to be deployed outside a local dev machine, the following changes would be required:

### Credentials
- Replace all fixed passwords with secrets from a secrets manager (Vault, AWS Secrets Manager, GCP Secret Manager)
- Rotate all credentials before first deployment
- Use distinct credentials per environment (dev/staging/prod)

### Network
- Bind service ports to internal interfaces only; expose only the ingest HTTP API through a load balancer or API gateway
- Add TLS termination at the ingress layer
- Place PostgreSQL and RabbitMQ in a private network segment unreachable from the public internet

### Database
- Enable SSL/TLS on PostgreSQL (`DB_SSL_MODE=require`)
- Restrict `pg_hba.conf` to application service IPs only
- Use a dedicated DB user with minimum required privileges (no `SUPERUSER`)

### RabbitMQ
- Enable TLS on AMQP connections
- Disable the management UI in production or place it behind auth/VPN
- Set per-user vhost permissions

### MinIO
- Replace with a managed object store (S3, GCS) for durability
- Enable server-side encryption and access logging
- Apply bucket policies restricting access to service IAM roles

### Grafana
- Change the default admin password immediately
- Disable user sign-up (`GF_USERS_ALLOW_SIGN_UP=false` is already set)
- Enable HTTPS

### Container Runtime
- Run all containers as non-root users
- Set read-only root filesystems where possible
- Apply resource limits (`mem_limit`, `cpus`) per container

---

## Risk Summary

| Risk | Local Dev Status | Production Mitigation Needed |
|------|-----------------|------------------------------|
| Hardcoded credentials | ✅ Acceptable (local only) | Replace with secrets manager |
| No TLS on DB | ✅ Acceptable (localhost) | Enable `DB_SSL_MODE=require` |
| RabbitMQ management UI exposed | ✅ Acceptable (localhost) | Restrict or disable |
| Grafana default password | ✅ Acceptable (localhost) | Change before deployment |
| SQL injection | ✅ Mitigated (parameterized queries) | No change needed |
| Payload tampering | ✅ Mitigated (SHA-256 verification) | No change needed |
| Duplicate processing | ✅ Mitigated (idempotency keys) | No change needed |
| Poison messages | ✅ Mitigated (NonRetryableError ACK) | No change needed |
| Secrets in logs | ✅ Mitigated (structured logging) | No change needed |
