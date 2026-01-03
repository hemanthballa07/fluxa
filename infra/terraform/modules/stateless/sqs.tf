# Dead Letter Queue (DLQ)
# Retention: 14 days - sufficient for investigation and manual redrive
resource "aws_sqs_queue" "dlq" {
  name = "${var.project_name}-dlq-${var.environment}"

  message_retention_seconds = 1209600 # 14 days - allows time for investigation

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-dlq-${var.environment}"
      Environment = var.environment
    }
  )
}

# Main SQS Queue
# Visibility timeout: 300s (5 min) - 6x processor Lambda timeout (50s) to prevent message re-delivery during processing
# Message retention: 4 days - default, sufficient for processing delays
# Long polling: 20s - reduces empty receives and API calls
# Redrive policy: maxReceiveCount=3 - permanent failures go to DLQ after 3 retries
resource "aws_sqs_queue" "main" {
  name = "${var.project_name}-queue-${var.environment}"

  visibility_timeout_seconds = 300 # 5 minutes - 6x processor timeout (50s) to prevent re-delivery
  message_retention_seconds  = 345600 # 4 days - default retention
  receive_wait_time_seconds  = 20 # Long polling - reduces empty receives

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3  # Permanent failures go to DLQ after 3 retries
  })

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-queue-${var.environment}"
      Environment = var.environment
    }
  )
}

