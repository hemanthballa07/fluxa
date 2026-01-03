# Release v1.1.0

**Date**: 2024-01-03

This release marks the completion of the core Fluxa roadmap, delivering a production-ready, observable, and infrastructurally hardened event processing system.

## Key Features & Improvements

### 🚀 Core Processing (Milestone B)
- **Hardened Validation**: Strict schema checks for timestamps, amounts, and metadata size.
- **Idempotency**: Verified exactly-once processing with high-concurrency tests (50 concurrent routines).
- **Failure Handling**: Robust handling of DB failures and hash mismatches (permanent failure marking).
- **Local Harness**: `make local-test` now runs against the *real* processor logic using Docker, not mocks.

### 👁️ Observability (Milestone C)
- **Structured Logging**: Standardized JSON logs across all services (`ingest`, `processor`, `query`) with trace IDs (`correlation_id`) and operational fields (`status`, `latency_ms`).
- **Unified Metrics**: CloudWatch EMF metrics aligned with log dimensions (`Service`, `Environment`).
- **Operational Runbook**: Added `docs/RUNBOOK.md` with guides for DLQ triage and safe replays.

### 🛡️ Infrastructure Hygiene (Milestone D)
- **Terraform CI**: Automated validation (`fmt`, `validate`) enforced via GitHub Actions.
- **Least-Privilege IAM**: Scoped permissions for Lambda (Logs, SQS, S3) to specific resources, removing unnecessary wildcards.
- **Security Verified**: S3/RDS encryption and strict public access blocks confirmed.
- **Docs**: New `infra/terraform/README.md` and `docs/TRADEOFFS.md`.

## Artifacts
- **Demo Guide**: See `docs/DEMO.md` for a step-by-step walkthrough.
- **Runbook**: See `docs/RUNBOOK.md` for operational procedures.
