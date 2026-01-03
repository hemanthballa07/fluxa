# CloudWatch Dashboard

This document explains how to import and use the CloudWatch Dashboard for Fluxa metrics visualization.

## Dashboard JSON

The dashboard JSON is located at: `infra/cloudwatch/dashboard.json`

This dashboard includes:

1. **Ingest Latency (p50, p95)** - Shows 50th and 95th percentile latency for event ingestion
2. **Processor Latency (p50, p95)** - Shows 50th and 95th percentile latency for event processing
3. **Ingest Success Rate (per minute)** - Count of successful ingestions per minute
4. **Processor Success Rate (per minute)** - Count of successfully processed events per minute
5. **Dead Letter Queue Depth** - Number of messages in the DLQ (with alert threshold at 1 message)

## Importing the Dashboard

### Method 1: AWS Console (Recommended)

1. **Open CloudWatch Console**:

   - Navigate to AWS Console → CloudWatch
   - Click "Dashboards" in the left sidebar
   - Click "Create dashboard"

2. **Import Dashboard JSON**:

   - Click "Actions" → "Import dashboard"
   - Copy the contents of `infra/cloudwatch/dashboard.json`
   - Paste into the JSON editor
   - **Important**: Update the following before importing:
     - Replace `"region": "us-east-1"` with your AWS region
     - Replace `"QueueName": "fluxa-dlq-dev"` with your actual DLQ name (format: `fluxa-dlq-{environment}`)

3. **Save Dashboard**:
   - Name: `Fluxa-{environment}` (e.g., `Fluxa-dev`)
   - Click "Create dashboard"

### Method 2: AWS CLI

```bash
# Update region and queue name in dashboard.json first
# Then import:
aws cloudwatch put-dashboard \
  --dashboard-name "Fluxa-dev" \
  --dashboard-body file://infra/cloudwatch/dashboard.json \
  --region us-east-1
```

### Method 3: Terraform (Future Enhancement)

The dashboard can be added as a Terraform resource in `infra/terraform/modules/stateless/cloudwatch.tf`:

```hcl
resource "aws_cloudwatch_dashboard" "fluxa" {
  dashboard_name = "${var.project_name}-${var.environment}"

  dashboard_body = jsonencode({
    widgets = [
      # ... dashboard widgets ...
    ]
  })
}
```

## Customizing Dashboard

### Update Region

Replace `"region": "us-east-1"` with your AWS region in all widget definitions.

### Update Queue Names

Replace `"QueueName": "fluxa-dlq-dev"` with your actual DLQ queue name. Queue names follow the pattern:

- DLQ: `fluxa-dlq-{environment}`
- Main Queue: `fluxa-queue-{environment}`

To find your queue names:

```bash
aws sqs list-queues --region us-east-1 | grep fluxa
```

Or from Terraform:

```bash
cd infra/terraform/envs/dev
terraform output -json | jq '.sqs_queue_name.value'
terraform output -json | jq '.sqs_dlq_name.value'
```

### Add More Metrics

You can add additional widgets to the dashboard by following the CloudWatch Dashboard JSON format. Common additions:

- Error rates (`ingest_failure`, `processed_failure`)
- Payload mode distribution (`payload_inline_count`, `payload_s3_count`)
- Database latency (`db_latency_ms`)

## Taking Screenshots for Portfolio

### Recommended Screenshots

1. **Latency Overview**:

   - Time range: Last 1 hour (or your load test window)
   - Widgets: Ingest Latency + Processor Latency (side by side)
   - Shows: p95 values clearly visible

2. **Throughput Overview**:

   - Time range: Last 1 hour
   - Widgets: Ingest Success Rate + Processor Success Rate
   - Shows: Events per minute during load test

3. **Health Overview**:
   - Time range: Last 24 hours
   - Widgets: All widgets in single view
   - Shows: System health over time

### Screenshot Best Practices

1. **Clear Labels**: Ensure all axis labels and legends are visible
2. **Time Range**: Include time range selector in screenshot if possible
3. **Annotations**: Use CloudWatch annotations to mark important events (load test start/end)
4. **High Resolution**: Take screenshots at full resolution for clarity
5. **Descriptive Filenames**: Save as `fluxa-dashboard-latency-{date}.png`, etc.

### Example Screenshot Workflow

1. Run load test: `./scripts/load_test.sh`
2. Wait 5 minutes for metrics to populate
3. Open CloudWatch Dashboard
4. Set time range to cover load test period
5. Add annotation at load test start time (optional)
6. Take screenshot
7. Save to `docs/screenshots/` directory

## Dashboard URL

After importing, the dashboard URL follows this pattern:

```
https://console.aws.amazon.com/cloudwatch/home?region={region}#dashboards:name=Fluxa-{environment}
```

## Troubleshooting

### Metrics Not Appearing

1. **Wait 2-5 minutes**: CloudWatch metrics can take time to appear
2. **Check Metric Names**: Verify metric names match exactly (`ingest_latency_ms`, `process_latency_ms`)
3. **Check Namespace**: Ensure namespace is `Fluxa/Ingest` or `Fluxa/Processor`
4. **Check Time Range**: Expand time range if metrics seem missing

### DLQ Metric Not Showing

1. **Verify Queue Name**: Queue name must match exactly (case-sensitive)
2. **Check Queue Exists**: Verify DLQ was created in Terraform
3. **Use Wildcard**: Try using `*` wildcard in queue name dimension if exact match fails

### Percentile Statistics Not Available

1. **Ensure Sufficient Data**: p50/p95 require multiple data points
2. **Check Period**: Use period 300 (5 minutes) or longer for percentile stats
3. **Use Extended Statistics**: Some metrics may require extended statistics

## Dashboard Maintenance

- **Update Region**: When deploying to new regions, update dashboard JSON
- **Update Queue Names**: When deploying to new environments, update queue name dimensions
- **Review Metrics**: Periodically review dashboard to ensure all metrics are still relevant
- **Cost**: CloudWatch dashboards are free, but querying metrics may incur costs

