# Ingest Lambda IAM Role
resource "aws_iam_role" "lambda_ingest" {
  name = "${var.project_name}-ingest-${var.environment}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "lambda_ingest" {
  name = "${var.project_name}-ingest-${var.environment}"
  role = aws_iam_role.lambda_ingest.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:log-group:/aws/lambda/${var.project_name}-*"
      },
      {
        Effect = "Allow"
        Action = [
          "sqs:SendMessage"
        ]
        Resource = aws_sqs_queue.main.arn
      },
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject"
        ]
        Resource = "${aws_s3_bucket.payloads.arn}/*"
      },
      {
        Effect = "Allow"
        Action = [
          "cloudwatch:PutMetricData"
        ]
        Resource = "*" # Wildcard with namespace condition - restricts to Fluxa/Ingest namespace only
        Condition = {
          StringEquals = {
            "cloudwatch:namespace" = "Fluxa/Ingest" # Namespace restriction mitigates wildcard risk
          }
        }
      }
    ]
  })
}

# Processor Lambda IAM Role
resource "aws_iam_role" "lambda_processor" {
  name = "${var.project_name}-processor-${var.environment}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "lambda_processor" {
  name = "${var.project_name}-processor-${var.environment}"
  role = aws_iam_role.lambda_processor.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:log-group:/aws/lambda/${var.project_name}-*"
      },
      {
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes"
        ]
        Resource = [
          aws_sqs_queue.main.arn,
          aws_sqs_queue.dlq.arn
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject"
        ]
        Resource = "${aws_s3_bucket.payloads.arn}/*"
      },
      {
        Effect = "Allow"
        Action = [
          "sns:Publish"
        ]
        Resource = aws_sns_topic.events.arn
      },
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue"
        ]
        Resource = var.db_password_secret_arn
      },
      {
        Effect = "Allow"
        Action = [
          "cloudwatch:PutMetricData"
        ]
        Resource = "*" # Wildcard with namespace condition - restricts to Fluxa/Processor namespace only
        Condition = {
          StringEquals = {
            "cloudwatch:namespace" = "Fluxa/Processor" # Namespace restriction mitigates wildcard risk
          }
        }
      }
    ]
  })
}

# VPC access policy for processor (if VPC is configured)
resource "aws_iam_role_policy_attachment" "lambda_processor_vpc" {
  count      = var.vpc_id != "" ? 1 : 0
  role       = aws_iam_role.lambda_processor.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

# Query Lambda IAM Role
resource "aws_iam_role" "lambda_query" {
  name = "${var.project_name}-query-${var.environment}"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "lambda_query" {
  name = "${var.project_name}-query-${var.environment}"
  role = aws_iam_role.lambda_query.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:log-group:/aws/lambda/${var.project_name}-*"
      },
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue"
        ]
        Resource = var.db_password_secret_arn
      },
      {
        Effect = "Allow"
        Action = [
          "cloudwatch:PutMetricData"
        ]
        Resource = "*" # Wildcard with namespace condition - restricts to Fluxa/Query namespace only
        Condition = {
          StringEquals = {
            "cloudwatch:namespace" = "Fluxa/Query" # Namespace restriction mitigates wildcard risk
          }
        }
      }
    ]
  })
}

# VPC access policy for query (if VPC is configured)
resource "aws_iam_role_policy_attachment" "lambda_query_vpc" {
  count      = var.vpc_id != "" ? 1 : 0
  role       = aws_iam_role.lambda_query.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

