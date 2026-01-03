# Architecture Trade-offs and Decisions

This document tracks architectural decisions and known trade-offs, particularly regarding security scope and infrastructure design.

## IAM Permissions

### 1. CloudWatch Metrics (`cloudwatch:PutMetricData`)
- **Scope**: `Resource: "*"`
- **Reason**: CloudWatch `PutMetricData` API does not support resource-level permissions (ARNs) for specific metrics or namespaces.
- **Mitigation**: We use an IAM `Condition` to restrict the `cloudwatch:namespace` to `Fluxa/*`. This ensures the role can only write metrics to our specific namespace, preventing pollution of other namespaces.

## Infrastructure

### 1. Standard vs. Express SQS
- **Decision**: Standard Queues
- **Reason**: Strict ordering is not a strict requirement for basic event processing as long as idempotency handles duplicates. Standard queues offer higher throughput and lower cost than FIFO queues.

### 2. Lambda Architecture (arm64)
- **Decision**: `arm64` architecture (Graviton2)
- **Reason**: Provides better price/performance ratio (approx 20% savings) compared to x86_64 for Go workloads.
