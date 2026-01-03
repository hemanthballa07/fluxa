# Ingest Lambda
# Timeout: 30s - sufficient for schema validation, payload hash, SQS send, optional S3 put
# Memory: 256MB - lightweight operation, no database access, arm64 for cost efficiency
resource "aws_lambda_function" "ingest" {
  filename         = var.lambda_ingest_zip_path
  function_name    = "${var.project_name}-ingest-${var.environment}"
  role            = aws_iam_role.lambda_ingest.arn
  handler         = "bootstrap"
  runtime         = "provided.al2"
  architectures   = ["arm64"]
  timeout         = 30  # 30 seconds - sufficient for validation + SQS send
  memory_size     = 256 # 256MB - lightweight, no DB operations

  source_code_hash = filebase64sha256(var.lambda_ingest_zip_path)

  environment {
    variables = {
      ENVIRONMENT      = var.environment
      SQS_QUEUE_URL    = aws_sqs_queue.main.url
      S3_BUCKET_NAME   = aws_s3_bucket.payloads.id
      LOG_LEVEL        = "info"
    }
  }

  dynamic "vpc_config" {
    for_each = var.vpc_id != "" ? [1] : []
    content {
      subnet_ids         = var.subnet_ids
      security_group_ids = [var.security_group_id]
    }
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-ingest-${var.environment}"
      Environment = var.environment
    }
  )
}

# Processor Lambda
# Timeout: 50s - allows for idempotency check, S3 fetch (if needed), DB insert, SNS publish
# Memory: 512MB - increased for DB operations and potential S3 payload fetch
# Note: SQS visibility timeout (300s) is set to 6x this timeout to prevent message re-delivery during processing
resource "aws_lambda_function" "processor" {
  filename         = var.lambda_processor_zip_path
  function_name    = "${var.project_name}-processor-${var.environment}"
  role            = aws_iam_role.lambda_processor.arn
  handler         = "bootstrap"
  runtime         = "provided.al2"
  architectures   = ["arm64"]
  timeout         = 50  # 50 seconds - DB operations + S3 fetch + SNS publish
  memory_size     = 512 # 512MB - DB connection pool + payload handling

  source_code_hash = filebase64sha256(var.lambda_processor_zip_path)

  environment {
    variables = {
      ENVIRONMENT      = var.environment
      SQS_QUEUE_URL    = aws_sqs_queue.main.url
      SQS_DLQ_URL      = aws_sqs_queue.dlq.url
      S3_BUCKET_NAME   = aws_s3_bucket.payloads.id
      DB_HOST          = var.db_host
      DB_PORT          = "5432"
      DB_NAME          = var.db_name
      DB_USER          = var.db_user
      DB_PASSWORD_SECRET_ARN = var.db_password_secret_arn
      DB_SSL_MODE      = "require"
      SNS_TOPIC_ARN    = aws_sns_topic.events.arn
      LOG_LEVEL        = "info"
    }
  }

  dynamic "vpc_config" {
    for_each = var.vpc_id != "" ? [1] : []
    content {
      subnet_ids         = var.subnet_ids
      security_group_ids = [var.security_group_id]
    }
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-processor-${var.environment}"
      Environment = var.environment
    }
  )
}

# Query Lambda
# Timeout: 30s - sufficient for single DB query operation
# Memory: 256MB - lightweight read-only operation
resource "aws_lambda_function" "query" {
  filename         = var.lambda_query_zip_path
  function_name    = "${var.project_name}-query-${var.environment}"
  role            = aws_iam_role.lambda_query.arn
  handler         = "bootstrap"
  runtime         = "provided.al2"
  architectures   = ["arm64"]
  timeout         = 30  # 30 seconds - single DB query operation
  memory_size     = 256 # 256MB - lightweight read-only operation

  source_code_hash = filebase64sha256(var.lambda_query_zip_path)

  environment {
    variables = {
      ENVIRONMENT   = var.environment
      DB_HOST       = var.db_host
      DB_PORT       = "5432"
      DB_NAME       = var.db_name
      DB_USER       = var.db_user
      DB_PASSWORD_SECRET_ARN = var.db_password_secret_arn
      DB_SSL_MODE   = "require"
      LOG_LEVEL     = "info"
    }
  }

  dynamic "vpc_config" {
    for_each = var.vpc_id != "" ? [1] : []
    content {
      subnet_ids         = var.subnet_ids
      security_group_ids = [var.security_group_id]
    }
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-query-${var.environment}"
      Environment = var.environment
    }
  )
}

# SQS Event Source Mapping for Processor Lambda
# Batch size: 10 (SQS max) - balances throughput with error handling granularity
# Note: If one message in batch fails, entire batch is retried. Consider individual error handling for production.
resource "aws_lambda_event_source_mapping" "processor" {
  event_source_arn = aws_sqs_queue.main.arn
  function_name    = aws_lambda_function.processor.arn
  batch_size       = 10  # Maximum batch size for better throughput
  enabled          = true
}

