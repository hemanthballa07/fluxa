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
    # key    = "fluxa/dev/terraform.tfstate"
    # region = "us-east-1"
  }
}

provider "aws" {
  region = var.aws_region
}

# Data source for default VPC (for simplicity in dev)
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# Security Group for Lambda (if using VPC)
resource "aws_security_group" "lambda" {
  name        = "fluxa-lambda-dev"
  description = "Security group for Lambda functions"
  vpc_id      = data.aws_vpc.default.id

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name        = "fluxa-lambda-dev"
    Environment = "dev"
  }
}

# Security Group for RDS - allows Lambda access
resource "aws_security_group" "rds" {
  name        = "fluxa-rds-dev"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = data.aws_vpc.default.id

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
    Name        = "fluxa-rds-dev"
    Environment = "dev"
  }
}

# Stateful module (RDS)
module "stateful" {
  source = "../../modules/stateful"

  environment         = "dev"
  project_name        = "fluxa"
  db_instance_class   = "db.t3.micro"
  db_name             = "fluxa"
  db_username         = var.db_username
  db_password         = var.db_password
  vpc_id              = data.aws_vpc.default.id
  subnet_ids          = data.aws_subnets.default.ids
  security_group_id   = aws_security_group.rds.id
  multi_az            = false
  skip_final_snapshot = true # For dev environment

  tags = {
    Environment = "dev"
    Project     = "fluxa"
  }
}

# Stateless module
module "stateless" {
  source = "../../modules/stateless"

  environment               = "dev"
  project_name              = "fluxa"
  lambda_ingest_zip_path    = var.lambda_ingest_zip_path
  lambda_processor_zip_path = var.lambda_processor_zip_path
  lambda_query_zip_path     = var.lambda_query_zip_path

  db_host                = module.stateful.db_address
  db_name                = module.stateful.db_name
  db_user                = module.stateful.db_username
  db_password_secret_arn = module.stateful.db_password_secret_arn

  s3_payload_bucket_name = "fluxa-payloads-dev-${random_id.bucket_suffix.hex}"

  # Use VPC for Lambda to connect to RDS (required for RDS access)
  vpc_id            = data.aws_vpc.default.id
  subnet_ids        = data.aws_subnets.default.ids
  security_group_id = aws_security_group.lambda.id

  tags = {
    Environment = "dev"
    Project     = "fluxa"
  }
}

# Random ID for unique bucket name
resource "random_id" "bucket_suffix" {
  byte_length = 4
}

