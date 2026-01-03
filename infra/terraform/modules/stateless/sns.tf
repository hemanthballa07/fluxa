resource "aws_sns_topic" "events" {
  name = "${var.project_name}-events-${var.environment}"

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-events-${var.environment}"
      Environment = var.environment
    }
  )
}


