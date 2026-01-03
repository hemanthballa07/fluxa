output "api_endpoint" {
  description = "API Gateway endpoint URL"
  value       = module.stateless.api_gateway_url
}

output "db_endpoint" {
  description = "RDS endpoint"
  value       = module.stateful.db_endpoint
}

output "sqs_queue_url" {
  description = "SQS queue URL"
  value       = module.stateless.sqs_queue_url
}

output "sns_topic_arn" {
  description = "SNS topic ARN"
  value       = module.stateless.sns_topic_arn
}


