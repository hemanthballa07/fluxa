output "db_endpoint" {
  description = "RDS endpoint address"
  value       = aws_db_instance.main.endpoint
}

output "db_address" {
  description = "RDS address (hostname)"
  value       = aws_db_instance.main.address
}

output "db_port" {
  description = "RDS port"
  value       = aws_db_instance.main.port
}

output "db_name" {
  description = "Database name"
  value       = aws_db_instance.main.db_name
}

output "db_username" {
  description = "Database master username"
  value       = aws_db_instance.main.username
  sensitive   = true
}

output "db_password_secret_arn" {
  description = "ARN of Secrets Manager secret containing DB password"
  value       = aws_secretsmanager_secret.db_password.arn
}

output "db_security_group_id" {
  description = "Security group ID for RDS"
  value       = var.security_group_id != "" ? var.security_group_id : aws_security_group.rds[0].id
}


