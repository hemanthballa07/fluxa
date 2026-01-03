variable "environment" {
  description = "Environment name (dev, prod)"
  type        = string
}

variable "project_name" {
  description = "Project name"
  type        = string
  default     = "fluxa"
}

variable "lambda_ingest_zip_path" {
  description = "Path to ingest Lambda deployment package"
  type        = string
}

variable "lambda_processor_zip_path" {
  description = "Path to processor Lambda deployment package"
  type        = string
}

variable "lambda_query_zip_path" {
  description = "Path to query Lambda deployment package"
  type        = string
}

variable "db_host" {
  description = "RDS PostgreSQL host"
  type        = string
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "fluxa"
}

variable "db_user" {
  description = "Database user"
  type        = string
}

variable "db_password_secret_arn" {
  description = "ARN of Secrets Manager secret containing DB password"
  type        = string
}

variable "s3_payload_bucket_name" {
  description = "S3 bucket name for payload storage"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID for Lambda (optional, leave empty for no VPC)"
  type        = string
  default     = ""
}

variable "subnet_ids" {
  description = "Subnet IDs for Lambda (required if VPC is used)"
  type        = list(string)
  default     = []
}

variable "security_group_id" {
  description = "Security group ID for Lambda (required if VPC is used)"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Resource tags"
  type        = map(string)
  default     = {}
}


