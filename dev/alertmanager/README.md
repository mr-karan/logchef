# Alertmanager Development Setup

This directory contains the Alertmanager configuration for local development and testing.

## Services

### Alertmanager
- **URL**: http://localhost:9093
- **Web UI**: http://localhost:9093/#/alerts
- **API**: http://localhost:9093/api/v2/alerts

### Webhook Receiver (Test Service)
- **URL**: http://localhost:8888
- **Webhook endpoint**: http://localhost:8888/webhook

The webhook receiver is a simple HTTP echo service that logs all incoming webhook requests. This allows you to see exactly what alerts are being sent by LogChef to Alertmanager.

## Configuration

### Alertmanager Configuration (`alertmanager.yml`)

The configuration includes:
- **Grouping**: Alerts are grouped by `alertname`, `severity`, `team`, and `source` (human-readable names)
- **Timing**:
  - `group_wait: 10s` - Wait 10 seconds before sending first notification
  - `group_interval: 30s` - Wait 30 seconds between updates to the same group
  - `repeat_interval: 12h` - Resend notification every 12 hours if still firing
- **Receiver**: All alerts route to the webhook-receiver service
- **Resolved Notifications**: Enabled (`send_resolved: true`)

### LogChef Configuration

In `config.toml`, the alerting section is configured as:

```toml
[alerts]
enabled = true
evaluation_interval = "1m"
alertmanager_url = "http://localhost:9093"
external_url = "http://localhost:8125"
```

## Testing Alerts

### 1. Start the services

```bash
just dev-docker
```

This will start:
- ClickHouse (logs database)
- Dex (authentication)
- Alertmanager (alert routing)
- Webhook Receiver (test endpoint)

### 2. View incoming alerts

Monitor the webhook receiver logs to see alerts as they arrive:

```bash
docker compose -f dev/docker-compose.yml logs -f webhook-receiver
```

You should see detailed HTTP POST requests with the alert payload whenever an alert fires.

### 3. Check Alertmanager UI

Open http://localhost:9093 in your browser to:
- View active alerts
- See alert grouping
- Check routing configuration
- Silence alerts (for testing)

### 4. Create a test alert in LogChef

1. Log in to LogChef at http://localhost:8125
2. Navigate to Alerts
3. Create a new alert with:
   - **Query**: `SELECT count(*) as value FROM logs WHERE severity_text = 'ERROR' AND timestamp >= now() - INTERVAL 5 MINUTE`
   - **Threshold**: Greater than 0
   - **Frequency**: 1 minute (for fast testing)

4. Wait for the evaluation cycle to run
5. Check the webhook receiver logs - you should see the alert payload

## Example Alert Payload

When an alert fires, the webhook receiver will log something like:

```json
{
  "receiver": "webhook-receiver",
  "status": "firing",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "high_error_rate",
        "alert_id": "1",
        "severity": "critical",
        "team": "Production Team",
        "team_id": "1",
        "source": "Main API Logs",
        "source_id": "1",
        "status": "triggered"
      },
      "annotations": {
        "description": "High error rate detected",
        "query": "SELECT count(*) as value FROM logs WHERE severity_text = 'ERROR'",
        "threshold": "gt 10.0000",
        "value": "42.0000"
      },
      "startsAt": "2025-01-17T12:00:00Z",
      "generatorURL": "http://localhost:5173/logs/alerts/1?team=1&source=1"
    }
  ],
  "groupLabels": {
    "alertname": "high_error_rate",
    "severity": "critical",
    "team": "Production Team",
    "source": "Main API Logs"
  }
}
```

## Customizing the Configuration

### Adding Email Notifications

To add email notifications, update `alertmanager.yml`:

```yaml
receivers:
  - name: 'webhook-receiver'
    webhook_configs:
      - url: 'http://webhook-receiver:8080/webhook'
    email_configs:
      - to: 'alerts@example.com'
        from: 'alertmanager@example.com'
        smarthost: 'smtp.example.com:587'
        auth_username: 'alerts@example.com'
        auth_password: 'your-password'
```

### Adding Slack Notifications

```yaml
receivers:
  - name: 'slack-receiver'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
        channel: '#alerts'
        title: 'LogChef Alert'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'
```

### Adding Multiple Routes

```yaml
route:
  receiver: 'default-receiver'
  group_by: ['alertname', 'severity', 'team', 'source']
  routes:
    # Critical alerts to PagerDuty
    - match:
        severity: critical
      receiver: 'pagerduty'
      continue: true  # Also send to default

    # Warning alerts only to Slack
    - match:
        severity: warning
      receiver: 'slack-receiver'

    # Route specific team alerts differently
    - match:
        team: 'Production Team'
      receiver: 'production-oncall'
      group_by: ['alertname', 'source']
```

## Troubleshooting

### Alerts not appearing in Alertmanager

1. Check LogChef logs for alert evaluation:
   ```bash
   just run-backend
   ```
   Look for log lines about alert evaluation

2. Verify alertmanager_url is correct in config.toml

3. Check Alertmanager is running:
   ```bash
   curl http://localhost:9093/-/healthy
   ```

### Webhook not receiving alerts

1. Check webhook receiver logs:
   ```bash
   docker compose -f dev/docker-compose.yml logs webhook-receiver
   ```

2. Test webhook manually:
   ```bash
   curl -X POST http://localhost:8888/webhook \
     -H "Content-Type: application/json" \
     -d '{"test": "alert"}'
   ```

3. Check Alertmanager logs:
   ```bash
   docker compose -f dev/docker-compose.yml logs alertmanager
   ```

## Production Considerations

For production use:

1. **Secure the webhook URL** - Use authentication/secrets
2. **Set up proper receivers** - PagerDuty, Slack, email, etc.
3. **Configure TLS** - Use HTTPS for Alertmanager
4. **Adjust timing** - Tune group_wait, group_interval, repeat_interval
5. **Add inhibit rules** - Prevent alert spam
6. **Set up high availability** - Run multiple Alertmanager instances
7. **Configure persistence** - Mount alertmanager-data volume properly
8. **Remove webhook-receiver** - Only needed for testing
