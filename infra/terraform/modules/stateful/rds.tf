# DB Subnet Group
resource "aws_db_subnet_group" "main" {
  name       = "${var.project_name}-db-subnet-${var.environment}"
  subnet_ids = var.subnet_ids

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-db-subnet-${var.environment}"
      Environment = var.environment
    }
  )
}

# Security Group for RDS (if not provided)
# Note: When security_group_id is provided, this is not created
# The provided security group should allow ingress from Lambda security groups
resource "aws_security_group" "rds" {
  count       = var.security_group_id == "" ? 1 : 0
  name        = "${var.project_name}-rds-${var.environment}"
  description = "Security group for RDS PostgreSQL"
  vpc_id      = var.vpc_id

  # Allow PostgreSQL access from VPC (will be restricted by Lambda security group in practice)
  ingress {
    description = "PostgreSQL from VPC"
    from_port   = var.db_port
    to_port     = var.db_port
    protocol    = "tcp"
    cidr_blocks = [] # Empty - should be restricted by source security groups in actual deployment
  }

  egress {
    description = "Allow all outbound"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-rds-${var.environment}"
      Environment = var.environment
    }
  )
}

# Secrets Manager Secret for DB Password
resource "aws_secretsmanager_secret" "db_password" {
  name        = "${var.project_name}/db-password/${var.environment}"
  description = "RDS PostgreSQL master password"

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-db-password-${var.environment}"
      Environment = var.environment
    }
  )
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = var.db_password
}

# RDS Instance
resource "aws_db_instance" "main" {
  identifier            = "${var.project_name}-db-${var.environment}"
  engine                = "postgres"
  engine_version        = "15.4"
  instance_class        = var.db_instance_class
  allocated_storage     = var.allocated_storage
  max_allocated_storage = var.max_allocated_storage
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = var.db_name
  username = var.db_username
  password = var.db_password
  port     = var.db_port

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [var.security_group_id != "" ? var.security_group_id : aws_security_group.rds[0].id]

  backup_retention_period = var.backup_retention_period
  backup_window           = "03:00-04:00"
  maintenance_window      = "mon:04:00-mon:05:00"

  multi_az            = var.multi_az
  publicly_accessible = false                                    # Always false - RDS in private subnets
  skip_final_snapshot = var.skip_final_snapshot                  # Only true for dev/test environments
  deletion_protection = var.environment == "prod" ? true : false # Protect prod from accidental deletion

  enabled_cloudwatch_logs_exports = ["postgresql", "upgrade"]

  performance_insights_enabled = var.environment == "prod"
  monitoring_interval          = var.environment == "prod" ? 60 : 0
  monitoring_role_arn          = var.environment == "prod" ? aws_iam_role.rds_enhanced_monitoring[0].arn : null

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-db-${var.environment}"
      Environment = var.environment
    }
  )
}

# Enhanced Monitoring IAM Role (for prod)
resource "aws_iam_role" "rds_enhanced_monitoring" {
  count = var.environment == "prod" ? 1 : 0
  name  = "${var.project_name}-rds-monitoring-${var.environment}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "monitoring.rds.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "rds_enhanced_monitoring" {
  count      = var.environment == "prod" ? 1 : 0
  role       = aws_iam_role.rds_enhanced_monitoring[0].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonRDSEnhancedMonitoringRole"
}

