# SummaryRule Backfill Examples

This directory contains examples and documentation for using the SummaryRule backfill feature.

## Examples

### Basic Usage
- **[summaryrule-backfill-basic.yaml](summaryrule-backfill-basic.yaml)** - Simple hourly aggregation with 30-day backfill
- **[summaryrule-backfill-advanced.yaml](summaryrule-backfill-advanced.yaml)** - Advanced example with ingestion delay and criteria
- **[summaryrule-backfill-migration.yaml](summaryrule-backfill-migration.yaml)** - Large-scale data migration using daily intervals

### Documentation
- **[backfill-troubleshooting.md](backfill-troubleshooting.md)** - Comprehensive troubleshooting guide

## Quick Start

1. **Apply a basic backfill rule:**
   ```bash
   kubectl apply -f summaryrule-backfill-basic.yaml
   ```

2. **Monitor progress:**
   ```bash
   # Watch the start time advance
   kubectl get summaryrule basic-backfill-example -o jsonpath='{.spec.backfill.start}' -w
   ```

3. **Check completion:**
   ```bash
   # Backfill is complete when the field is removed
   kubectl get summaryrule basic-backfill-example -o jsonpath='{.spec.backfill}' 2>/dev/null || echo "Backfill complete"
   ```

## Key Features

- **Parallel Execution**: Backfill runs alongside normal interval execution
- **Automatic Progress**: System advances through time windows automatically
- **Auto-cleanup**: Backfill field is removed when complete
- **30-day Limit**: Maximum lookback period prevents excessive cluster load
- **Serial Processing**: One time window at a time prevents cluster overload

## Important Notes

- Backfill operations respect the same `ingestionDelay` as normal execution
- Maximum backfill period is 30 days
- Users are responsible for handling any data overlap concerns
- Monitor cluster performance during large backfill operations

For detailed information, see the [CRD documentation](../crds.md#backfill-feature-for-historical-data-processing).
