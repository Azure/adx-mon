# SummaryRule Backfill Troubleshooting Guide

This guide helps diagnose and resolve common issues with SummaryRule backfill operations.

## Common Issues and Solutions

### Backfill Not Starting

**Symptoms:**
- Backfill field is present but `spec.backfill.start` never advances
- No `operationId` appears in the backfill spec

**Possible Causes & Solutions:**

1. **Invalid Date Format**
   ```bash
   # Check the SummaryRule for validation errors
   kubectl describe summaryrule <rule-name> -n <namespace>
   ```
   - Ensure dates are in RFC3339 format: `2024-01-01T00:00:00Z`
   - Verify start < end and within 30-day limit

2. **Criteria Mismatch**
   - Check if the rule's criteria match the ingestor's cluster labels
   - Verify ingestor is running with appropriate `--cluster-labels`

3. **Rule Execution Prerequisites**
   - Ensure the rule has had at least one normal execution first
   - Check that the database and table exist in ADX

### Backfill Stuck in Progress

**Symptoms:**
- `spec.backfill.operationId` is present but not changing
- `spec.backfill.start` not advancing

**Diagnosis Steps:**

1. **Check Operation Status in ADX**
   ```kql
   .show operations
   | where OperationId == "<operation-id-from-spec>"
   | project State, Status, Details
   ```

2. **Check Ingestor Logs**
   ```bash
   kubectl logs -f deployment/ingestor -n adx-mon | grep -i backfill
   ```

**Solutions:**

- **Operation Failed**: The operation will retry automatically when the `operationId` is cleared
- **Operation Throttled**: Wait for ADX cluster resources to become available
- **Operation Timeout**: Operations older than 24 hours are automatically cleaned up

### Backfill Performance Issues

**Symptoms:**
- ADX cluster experiencing high load
- Query timeouts or throttling errors
- Normal rule execution being affected

**Mitigation Strategies:**

1. **Increase Interval Size**
   ```yaml
   spec:
     interval: 1d  # Process larger chunks less frequently
   ```

2. **Add Ingestion Delay**
   ```yaml
   spec:
     ingestionDelay: 1h  # Reduce temporal overlap
   ```

3. **Schedule During Off-Peak Hours**
   - Temporarily remove backfill field during peak hours
   - Re-add during low-traffic periods

4. **Optimize KQL Query**
   - Add filters to reduce data volume
   - Use efficient aggregation functions
   - Consider pre-filtering with materialized views

### Invalid Backfill Configuration

**Symptoms:**
- Kubernetes validation errors when applying the manifest
- CRD events showing validation failures

**Common Validation Errors:**

1. **Date Range Too Large**
   ```
   Error: backfill period exceeds maximum 30-day limit
   ```
   - Reduce the time span between start and end dates

2. **Start After End**
   ```
   Error: backfill start must be before end
   ```
   - Verify the start date is earlier than the end date

3. **Missing Required Fields**
   ```
   Error: backfill start and end are required when backfill is specified
   ```
   - Ensure both `start` and `end` fields are provided

### Monitoring Backfill Progress

**Progress Tracking:**
```bash
# Watch backfill progress
kubectl get summaryrule <rule-name> -o jsonpath='{.spec.backfill.start}' -w

# Check current operation
kubectl get summaryrule <rule-name> -o jsonpath='{.spec.backfill.operationId}'
```

**Completion Detection:**
```bash
# Backfill is complete when the field is removed
kubectl get summaryrule <rule-name> -o jsonpath='{.spec.backfill}' || echo "Backfill complete"
```

**ADX Operation Monitoring:**
```kql
// Check recent backfill operations
.show operations
| where CommandType == "DataIngestPull"
| where Text contains "<table-name>"
| project StartedOn, CompletedOn, State, OperationId
| order by StartedOn desc
```

## Best Practices for Reliable Backfill

### 1. Plan Your Backfill Strategy
- Start with smaller time ranges to test performance
- Consider the impact on cluster resources
- Schedule backfill during off-peak hours

### 2. Monitor Resource Usage
- Watch ADX cluster CPU and memory usage
- Monitor query performance and timeouts
- Check for throttling in ADX metrics

### 3. Optimize for Performance
- Use appropriate interval sizes (larger intervals = fewer operations)
- Add filters to reduce data volume
- Consider ingestion delays to avoid temporal overlap

### 4. Handle Failures Gracefully
- Failed operations will retry automatically
- Monitor logs for persistent failures
- Consider adjusting query complexity if timeouts occur

### 5. Test Before Production
- Test backfill on development clusters first
- Validate query performance with sample data
- Verify time window calculations with small ranges

## Advanced Troubleshooting

### Manual Operation Cleanup
If an operation gets stuck and doesn't clear automatically:

```bash
# Edit the SummaryRule to clear the operationId
kubectl patch summaryrule <rule-name> --type='json' \
  -p='[{"op": "remove", "path": "/spec/backfill/operationId"}]'
```

### Force Backfill Completion
To manually complete a backfill:

```bash
# Remove the entire backfill field
kubectl patch summaryrule <rule-name> --type='json' \
  -p='[{"op": "remove", "path": "/spec/backfill"}]'
```

### Reset Backfill Progress
To restart backfill from a specific point:

```bash
# Update the start time
kubectl patch summaryrule <rule-name> --type='json' \
  -p='[{"op": "replace", "path": "/spec/backfill/start", "value": "2024-01-15T00:00:00Z"}]'
```

## Getting Help

If you encounter issues not covered in this guide:

1. Check the [SummaryRule documentation](../crds.md#summaryrule)
2. Review ingestor logs for detailed error messages
3. Examine ADX operation details for query-specific issues
4. Consider reducing backfill scope or optimizing queries
