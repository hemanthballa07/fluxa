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

  # Using local backend for audit deployment
  backend "local" {
    path = "terraform.tfstate"
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

  ingress {
    description = "Allow HTTPS from self (for VPC Endpoints)"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    self        = true
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

  environment           = "dev"
  project_name          = "fluxa"
  db_instance_class     = "db.t3.micro"
  db_name               = "fluxa"
  db_username           = var.db_username
  db_password           = var.db_password
  vpc_id                = data.aws_vpc.default.id
  subnet_ids            = data.aws_subnets.default.ids
  security_group_id     = aws_security_group.rds.id
  create_security_group = false
  multi_az              = false
  skip_final_snapshot   = true # For dev environment

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

# VPC Endpoints for Lambda connectivity (since running in public subnet without NAT)
resource "aws_vpc_endpoint" "s3" {
  vpc_id            = data.aws_vpc.default.id
  service_name      = "com.amazonaws.us-east-1.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [data.aws_vpc.default.main_route_table_id]

  tags = {
    Name = "fluxa-vpce-s3"
  }
}

resource "aws_vpc_endpoint" "secretsmanager" {
  vpc_id              = data.aws_vpc.default.id
  service_name        = "com.amazonaws.us-east-1.secretsmanager"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = data.aws_subnets.default.ids
  security_group_ids  = [aws_security_group.lambda.id]
  private_dns_enabled = true

  tags = {
    Name = "fluxa-vpce-secretsmanager"
  }
}

resource "aws_vpc_endpoint" "sqs" {
  vpc_id              = data.aws_vpc.default.id
  service_name        = "com.amazonaws.us-east-1.sqs"
  vpc_endpoint_type   = "Interface"
  subnet_ids          = data.aws_subnets.default.ids
  security_group_ids  = [aws_security_group.lambda.id]
  private_dns_enabled = true

  tags = {
    Name = "fluxa-vpce-sqs"
  }
}

