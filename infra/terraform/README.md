# Fluxa Infrastructure

This directory contains the Terraform configuration for Fluxa's infrastructure.

## Structure

- **`envs/`**: Environment-specific configurations (Dev, Prod).
    - `dev`: Default development environment.
    - `prod`: Production environment (with deletion protection, monitoring).
- **`modules/`**: Reusable infrastructure components.
    - `stateless`: Lambda, API Gateway, SQS, SNS, IAM (cheap/fast to destroy).
    - `stateful`: RDS, S3, VPC (persistent data).

## Deployment

### Prerequisites
- Terraform >= 1.5.0
- AWS Credentials configured (`aws configure`)

### Commands
Use the root `Makefile` for convenience:
```bash
# Validate Terraform syntax and configuration
make terraform-validate

# Deploy to Dev
make deploy-dev

# Deploy to Prod
make deploy-prod
```

## Security & Usage
- **Secrets**: DB Passwords are managed via AWS Secrets Manager.
- **Access**: All databases are in private subnets. Access is only possible via Lambda or Bastion (not configured).
- **Encryption**: S3 and RDS are encrypted at rest (AES-256).
- **Cost Guardrails**:
    - Dev uses `db.t3.micro` (RDS) and `arm64` Lambdas.
    - Prod uses `db.t3.small` and enables Enhanced Monitoring.

## Destroying
To tear down an environment:
```bash
cd infra/terraform/envs/dev
terraform destroy
```
*Note: S3 buckets and RDS (prod) may have deletion protection enabled.*
