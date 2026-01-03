# DLQ Depth Alarm
resource "aws_cloudwatch_metric_alarm" "dlq_depth" {
  alarm_name          = "${var.project_name}-dlq-depth-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 300
  statistic           = "Average"
  threshold           = 0
  alarm_description   = "Alerts when messages appear in DLQ"
  treat_missing_data  = "notBreaching"

  dimensions = {
    QueueName = aws_sqs_queue.dlq.name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-dlq-depth-${var.environment}"
      Environment = var.environment
    }
  )
}

# Lambda Error Rate Alarms
resource "aws_cloudwatch_metric_alarm" "lambda_ingest_errors" {
  alarm_name          = "${var.project_name}-ingest-errors-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "Alerts when Ingest Lambda error rate is high"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.ingest.function_name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-ingest-errors-${var.environment}"
      Environment = var.environment
    }
  )
}

resource "aws_cloudwatch_metric_alarm" "lambda_processor_errors" {
  alarm_name          = "${var.project_name}-processor-errors-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "Alerts when Processor Lambda error rate is high"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.processor.function_name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-processor-errors-${var.environment}"
      Environment = var.environment
    }
  )
}

resource "aws_cloudwatch_metric_alarm" "lambda_query_errors" {
  alarm_name          = "${var.project_name}-query-errors-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 5
  alarm_description   = "Alerts when Query Lambda error rate is high"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.query.function_name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-query-errors-${var.environment}"
      Environment = var.environment
    }
  )
}

# Lambda Throttle Alarms
resource "aws_cloudwatch_metric_alarm" "lambda_ingest_throttles" {
  alarm_name          = "${var.project_name}-ingest-throttles-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Throttles"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  alarm_description   = "Alerts when Ingest Lambda is throttled"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.ingest.function_name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-ingest-throttles-${var.environment}"
      Environment = var.environment
    }
  )
}

resource "aws_cloudwatch_metric_alarm" "lambda_processor_throttles" {
  alarm_name          = "${var.project_name}-processor-throttles-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Throttles"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  alarm_description   = "Alerts when Processor Lambda is throttled"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.processor.function_name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-processor-throttles-${var.environment}"
      Environment = var.environment
    }
  )
}

# API Gateway 5xx Errors Alarm
resource "aws_cloudwatch_metric_alarm" "api_5xx_errors" {
  alarm_name          = "${var.project_name}-api-5xx-${var.environment}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "5XXError"
  namespace           = "AWS/ApiGateway"
  period              = 300
  statistic           = "Sum"
  threshold           = 10
  alarm_description   = "Alerts when API Gateway returns 5xx errors"
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiName = aws_api_gateway_rest_api.api.name
    Stage   = aws_api_gateway_stage.api.stage_name
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-api-5xx-${var.environment}"
      Environment = var.environment
    }
  )
}


