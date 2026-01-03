variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs (at least 2 for Multi-AZ RDS)"
  type        = list(string)
}

variable "db_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.small"
}

variable "db_username" {
  description = "Database master username"
  type        = string
  default     = "fluxa_admin"
}

variable "db_password" {
  description = "Database master password"
  type        = string
  sensitive   = true
}

variable "lambda_ingest_zip_path" {
  description = "Path to ingest Lambda deployment package"
  type        = string
  default     = "../../../../dist/ingest.zip"
}

variable "lambda_processor_zip_path" {
  description = "Path to processor Lambda deployment package"
  type        = string
  default     = "../../../../dist/processor.zip"
}

variable "lambda_query_zip_path" {
  description = "Path to query Lambda deployment package"
  type        = string
  default     = "../../../../dist/query.zip"
}


