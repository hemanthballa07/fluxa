resource "aws_api_gateway_rest_api" "api" {
  name        = "${var.project_name}-api-${var.environment}"
  description = "Fluxa Event API"

  endpoint_configuration {
    types = ["REGIONAL"]
  }

  tags = merge(
    var.tags,
    {
      Name        = "${var.project_name}-api-${var.environment}"
      Environment = var.environment
    }
  )
}

resource "aws_api_gateway_resource" "events" {
  rest_api_id = aws_api_gateway_rest_api.api.id
  parent_id   = aws_api_gateway_rest_api.api.root_resource_id
  path_part   = "events"
}

resource "aws_api_gateway_resource" "events_id" {
  rest_api_id = aws_api_gateway_rest_api.api.id
  parent_id   = aws_api_gateway_resource.events.id
  path_part   = "{event_id}"
}

resource "aws_api_gateway_resource" "health" {
  rest_api_id = aws_api_gateway_rest_api.api.id
  parent_id   = aws_api_gateway_rest_api.api.root_resource_id
  path_part   = "health"
}

# POST /events
resource "aws_api_gateway_method" "post_events" {
  rest_api_id   = aws_api_gateway_rest_api.api.id
  resource_id   = aws_api_gateway_resource.events.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "post_events" {
  rest_api_id = aws_api_gateway_rest_api.api.id
  resource_id = aws_api_gateway_resource.events.id
  http_method = aws_api_gateway_method.post_events.http_method

  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.ingest.invoke_arn
}

# GET /events/{event_id}
resource "aws_api_gateway_method" "get_event" {
  rest_api_id   = aws_api_gateway_rest_api.api.id
  resource_id   = aws_api_gateway_resource.events_id.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "get_event" {
  rest_api_id = aws_api_gateway_rest_api.api.id
  resource_id = aws_api_gateway_resource.events_id.id
  http_method = aws_api_gateway_method.get_event.http_method

  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.query.invoke_arn
}

# GET /health
resource "aws_api_gateway_method" "get_health" {
  rest_api_id   = aws_api_gateway_rest_api.api.id
  resource_id   = aws_api_gateway_resource.health.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "get_health" {
  rest_api_id = aws_api_gateway_rest_api.api.id
  resource_id = aws_api_gateway_resource.health.id
  http_method = aws_api_gateway_method.get_health.http_method

  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.query.invoke_arn
}

# Lambda permissions
resource "aws_lambda_permission" "ingest" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ingest.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api.execution_arn}/*/*"
}

resource "aws_lambda_permission" "query" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.query.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api.execution_arn}/*/*"
}

# Deployment
resource "aws_api_gateway_deployment" "api" {
  depends_on = [
    aws_api_gateway_integration.post_events,
    aws_api_gateway_integration.get_event,
    aws_api_gateway_integration.get_health,
  ]

  rest_api_id = aws_api_gateway_rest_api.api.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_resource.events.id,
      aws_api_gateway_resource.events_id.id,
      aws_api_gateway_resource.health.id,
      aws_api_gateway_method.post_events.id,
      aws_api_gateway_method.get_event.id,
      aws_api_gateway_method.get_health.id,
      aws_api_gateway_integration.post_events.id,
      aws_api_gateway_integration.get_event.id,
      aws_api_gateway_integration.get_health.id,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_stage" "api" {
  deployment_id = aws_api_gateway_deployment.api.id
  rest_api_id   = aws_api_gateway_rest_api.api.id
  stage_name    = var.environment
}


