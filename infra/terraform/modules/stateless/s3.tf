resource "aws_s3_bucket" "payloads" {
  bucket = var.s3_payload_bucket_name

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-payloads-${var.environment}"
      Environment = var.environment
    }
  )
}

resource "aws_s3_bucket_versioning" "payloads" {
  bucket = aws_s3_bucket.payloads.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "payloads" {
  bucket = aws_s3_bucket.payloads.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "payloads" {
  bucket = aws_s3_bucket.payloads.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "payloads" {
  bucket = aws_s3_bucket.payloads.id

  rule {
    id     = "archive_old_payloads"
    status = "Enabled"

    expiration {
      days = 90
    }

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}


