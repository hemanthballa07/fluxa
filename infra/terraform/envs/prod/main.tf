terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }

  backend "s3" {
    # Configure backend in terraform.tfvars or via CLI
    # bucket = "your-terraform-state-bucket"
    # key    = "fluxa/prod/terraform.tfstate"
    # region = "us-east-1"
  }
}

provider "aws" {
  region = var.aws_region
}

# VPC Configuration (should be provided or created separately)
# For production, use dedicated VPC with proper networking
variable "vpc_id" {
  description = "VPC ID for production deployment"
  type        = string
}

variable "subnet_ids" {
  description = "Subnet IDs for RDS and Lambda"
  type        = list(string)
}

# Security Group for Lambda
resource "aws_security_group" "lambda" {
  name        = "fluxa-lambda-prod"
  description = "Security group for Lambda functions"
  vpc_id      = var.vpc_id

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name        = "fluxa-lambda-prod"
    Environment = "prod"
  }
}

# Security Group for RDS
resource "aws_security_group" "rds" {
  name        = "fluxa-rds-prod"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = var.vpc_id

  ingress {
    description     = "PostgreSQL from Lambda"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name        = "fluxa-rds-prod"
    Environment = "prod"
  }
}

# Stateful module (RDS)
module "stateful" {
  source = "../../modules/stateful"

  environment             = "prod"
  project_name            = "fluxa"
  db_instance_class       = var.db_instance_class
  db_name                 = "fluxa"
  db_username             = var.db_username
  db_password             = var.db_password
  vpc_id                  = var.vpc_id
  subnet_ids              = var.subnet_ids
  security_group_id       = aws_security_group.rds.id
  multi_az                = true
  skip_final_snapshot     = false
  backup_retention_period = 30

  tags = {
    Environment = "prod"
    Project     = "fluxa"
  }
}

# Stateless module
module "stateless" {
  source = "../../modules/stateless"

  environment               = "prod"
  project_name              = "fluxa"
  lambda_ingest_zip_path    = var.lambda_ingest_zip_path
  lambda_processor_zip_path = var.lambda_processor_zip_path
  lambda_query_zip_path     = var.lambda_query_zip_path

  db_host                = module.stateful.db_address
  db_name                = module.stateful.db_name
  db_user                = module.stateful.db_username
  db_password_secret_arn = module.stateful.db_password_secret_arn

  s3_payload_bucket_name = "fluxa-payloads-prod-${random_id.bucket_suffix.hex}"

  vpc_id            = var.vpc_id
  subnet_ids        = var.subnet_ids
  security_group_id = aws_security_group.lambda.id

  tags = {
    Environment = "prod"
    Project     = "fluxa"
  }
}

# Random ID for unique bucket name
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

