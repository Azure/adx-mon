# write-metric

A command-line tool for sending single OTLP metrics directly to an OTLP/HTTP endpoint.

## Usage

```bash
write-metric --input metric.json --endpoint https://otlp.example.com/v1/metrics
```

### Options

- `--input`: Path to JSON file containing OTLP metric (required)
- `--endpoint`: OTLP/HTTP endpoint URL (required, unless using --dry-run)
- `--dry-run`: Validate input and show what would be sent, but don't actually send
- `--verbose`: Enable verbose logging
- `--timeout`: HTTP timeout (default: 30s)
- `--insecure-skip-verify`: Skip TLS certificate verification

### JSON Format

The tool accepts OTLP metrics in JSON format. Here are examples for different metric types:

#### Counter/Sum Metric
```json
{
  "name": "http_requests_total",
  "sum": {
    "dataPoints": [{
      "value": "42",
      "timeUnixNano": "1640995200000000000",
      "attributes": [
        {"key": "method", "value": "GET"},
        {"key": "status", "value": "200"}
      ]
    }],
    "aggregationTemporality": 2,
    "isMonotonic": true
  }
}
```

#### Gauge Metric
```json
{
  "name": "cpu_usage",
  "gauge": {
    "dataPoints": [{
      "value": 75.5,
      "timeUnixNano": "1640995200000000000",
      "attributes": [
        {"key": "host", "value": "server-01"},
        {"key": "cpu", "value": "cpu0"}
      ]
    }]
  }
}
```

#### Histogram Metric
```json
{
  "name": "http_request_duration_seconds",
  "histogram": {
    "dataPoints": [{
      "count": "100",
      "sum": 45.67,
      "timeUnixNano": "1640995200000000000",
      "bucketCounts": ["10", "25", "40", "20", "5"],
      "explicitBounds": [0.1, 0.5, 1.0, 2.0, 5.0],
      "attributes": [
        {"key": "endpoint", "value": "/api/users"}
      ]
    }],
    "aggregationTemporality": 2
  }
}
```

### Examples

1. **Dry run validation:**
   ```bash
   write-metric --input metric.json --dry-run --verbose
   ```

2. **Send to local OTLP endpoint:**
   ```bash
   write-metric --input metric.json --endpoint http://localhost:4318/v1/metrics
   ```

3. **Send with custom timeout:**
   ```bash
   write-metric --input metric.json --endpoint https://otlp.example.com/v1/metrics --timeout 60s
   ```

### Error Handling

The tool performs validation and provides clear error messages:

- Invalid JSON format
- Missing required fields (metric name)
- Invalid timestamp formats
- Network/HTTP errors
- Server response errors

All errors result in a non-zero exit code for integration with scripts and CI/CD pipelines.
