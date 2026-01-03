output "api_gateway_url" {
  description = "API Gateway endpoint URL"
  value       = "${aws_api_gateway_deployment.api.invoke_url}/${aws_api_gateway_stage.api.stage_name}"
}

output "sqs_queue_url" {
  description = "SQS queue URL"
  value       = aws_sqs_queue.main.url
}

output "sqs_dlq_url" {
  description = "SQS DLQ URL"
  value       = aws_sqs_queue.dlq.url
}

output "sns_topic_arn" {
  description = "SNS topic ARN"
  value       = aws_sns_topic.events.arn
}

output "s3_bucket_name" {
  description = "S3 bucket name for payloads"
  value       = aws_s3_bucket.payloads.id
}

output "lambda_ingest_function_name" {
  description = "Ingest Lambda function name"
  value       = aws_lambda_function.ingest.function_name
}

output "lambda_processor_function_name" {
  description = "Processor Lambda function name"
  value       = aws_lambda_function.processor.function_name
}

output "lambda_query_function_name" {
  description = "Query Lambda function name"
  value       = aws_lambda_function.query.function_name
}

