// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package adx

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/Azure/adx-mon/ingestor/cluster"
	"github.com/Azure/adx-mon/metrics"
	"github.com/Azure/adx-mon/pkg/logger"
	adxschema "github.com/Azure/adx-mon/schema"
	azkustoingest "github.com/Azure/azure-kusto-go/azkustoingest"
)

const (
	// maxStreamingRequestBytes is the ADX streaming ingestion request size limit for uncompressed data.
	maxStreamingRequestBytes int64 = 4 * 1024 * 1024

	defaultStreamingCooldown       = 5 * time.Minute
	defaultStreamingRequestTimeout = 30 * time.Second

	// maxSlotWait bounds how long an upload worker waits for a streaming slot.  Batches that cannot get a slot are
	// released and retried so they wait in the WAL rather than holding workers that could upload queued batches.
	maxSlotWait = 250 * time.Millisecond
)

// RealtimeUploadOpts configures streaming ingestion of realtime batches.
type RealtimeUploadOpts struct {
	// Slots limits concurrent streaming requests to the uploader's Kusto endpoint.  It may be shared by uploaders
	// for databases on the same endpoint.
	Slots *StreamingSlots

	// MaxLag is the maximum age of a realtime batch's oldest segment before it uses queued ingestion.
	MaxLag time.Duration

	// MaxRequestBytes is the maximum uncompressed size of a streaming request.  Larger batches use queued ingestion.
	// Defaults to 4MiB, the streaming ingestion limit.
	MaxRequestBytes int64

	// Cooldown is how long a table uses queued ingestion after streaming ingestion is unavailable for it.  Defaults
	// to 5 minutes.
	Cooldown time.Duration

	// RequestTimeout is the timeout of a streaming request.  Defaults to 30 seconds.
	RequestTimeout time.Duration
}

func (o *RealtimeUploadOpts) withDefaults() *RealtimeUploadOpts {
	opts := *o
	if opts.MaxRequestBytes <= 0 || opts.MaxRequestBytes > maxStreamingRequestBytes {
		opts.MaxRequestBytes = maxStreamingRequestBytes
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = defaultStreamingCooldown
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = defaultStreamingRequestTimeout
	}
	return &opts
}

// streamIngester ingests data with streaming ingestion.  It is implemented by *azkustoingest.Streaming.
type streamIngester interface {
	FromReader(ctx context.Context, reader io.Reader, options ...azkustoingest.FileOption) (*azkustoingest.Result, error)
	Close() error
}

// streamOutcome is the result of attempting to stream a realtime batch.
type streamOutcome int

const (
	// streamed means the batch was ingested with streaming ingestion.
	streamed streamOutcome = iota
	// streamRetry means the batch should be retried later with streaming ingestion.
	streamRetry
	// streamFallback means the batch should be ingested with queued ingestion now.
	streamFallback
)

// streamingCooldowns tracks tables that use queued ingestion because streaming ingestion is unavailable.
type streamingCooldowns struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (c *streamingCooldowns) active(table string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.until[table]
	if ok && !now.Before(until) {
		delete(c.until, table)
		return false
	}
	return ok
}

func (c *streamingCooldowns) start(table string, until time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.until == nil {
		c.until = make(map[string]time.Time)
	}
	c.until[table] = until
}

// streamBatch attempts to ingest a realtime batch with streaming ingestion.  data is the batch's uncompressed CSV
// data.  When the outcome is streamFallback, the returned reader yields the batch's data for queued ingestion.
func (n *uploader) streamBatch(batch *cluster.Batch, table string, mapping adxschema.SchemaMapping, data io.Reader) (streamOutcome, io.Reader) {
	rt := n.realtime
	now := time.Now()
	oldest := oldestSegment(batch)

	switch {
	case n.requireDirectIngest:
		return n.fallback(table, "direct_ingest", data)
	case n.cooldowns.active(table, now):
		return n.fallback(table, "cooldown", data)
	case rt.MaxLag > 0 && now.Sub(oldest) >= rt.MaxLag:
		return n.fallback(table, "lag", data)
	}

	// Streaming requests are limited by the uncompressed size, which is only known after reading the segments.
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, io.LimitReader(data, rt.MaxRequestBytes+1)); err != nil {
		logger.Errorf("Failed to read realtime batch db=%s table=%s: %s", n.database, table, err)
		return n.retry(table, "read_error")
	}
	if int64(buf.Len()) > rt.MaxRequestBytes {
		return n.fallback(table, "too_large", io.MultiReader(buf, data))
	}
	if buf.Len() == 0 {
		n.recordRealtime(table, "streamed")
		return streamed, nil
	}
	buffered := buf.Bytes()

	if err := n.syncer.EnsureTable(table, mapping); err != nil {
		logger.Errorf("Failed to ensure table db=%s table=%s: %s", n.database, table, err)
		return n.fallback(table, "schema", bytes.NewReader(buffered))
	}
	mappingName, err := n.syncer.EnsureMapping(table, mapping)
	if err != nil {
		logger.Errorf("Failed to ensure mapping db=%s table=%s: %s", n.database, table, err)
		return n.fallback(table, "schema", bytes.NewReader(buffered))
	}
	if err := n.syncer.EnsureStreamingPolicy(n.ctx, table); err != nil {
		// The policy may be enabled at the database level so streaming is still attempted.
		logger.Warnf("%s", err)
	}

	// Wait briefly for a slot.  If the batch reaches its max lag first it uses queued ingestion, otherwise it is
	// retried later.
	slotDeadline := now.Add(maxSlotWait)
	lagDeadline := oldest.Add(rt.MaxLag)
	if rt.MaxLag > 0 && lagDeadline.Before(slotDeadline) {
		slotDeadline = lagDeadline
	}
	slotCtx, cancel := context.WithDeadline(n.ctx, slotDeadline)
	release, err := rt.Slots.Acquire(slotCtx)
	cancel()
	if err != nil {
		if n.ctx.Err() != nil {
			return n.retry(table, "shutdown")
		}
		if rt.MaxLag <= 0 || time.Now().Before(lagDeadline) {
			return n.retry(table, "no_slot")
		}
		return n.fallback(table, "lag", bytes.NewReader(buffered))
	}

	ctx, cancel := context.WithTimeout(n.ctx, rt.RequestTimeout)
	start := time.Now()
	_, err = n.streamer.FromReader(ctx, bytes.NewReader(buffered),
		azkustoingest.Database(n.database),
		azkustoingest.Table(table),
		azkustoingest.IngestionMappingRef(mappingName, azkustoingest.CSV),
	)
	cancel()
	metrics.IngestorRealtimeStreamingRequests.WithLabelValues(n.database).Inc()
	metrics.IngestorRealtimeStreamingDuration.WithLabelValues(n.database).Add(time.Since(start).Seconds())

	if err == nil {
		release(false)
		n.recordRealtime(table, "streamed")
		metrics.IngestorRealtimeIngestLatency.WithLabelValues(n.database, table).Set(time.Since(oldest).Seconds())
		if logger.IsDebug() {
			logger.Debugf("Streamed db=%s table=%s bytes=%d duration=%s", n.database, table, len(buffered), time.Since(start))
		}
		return streamed, nil
	}

	failure := classifyStreamingError(err)
	release(failure == streamingThrottled)
	logger.Warnf("Streaming ingestion failed db=%s table=%s failure=%s: %s", n.database, table, failure, sanitizeErrorString(err))

	switch failure {
	case streamingThrottled:
		return n.retry(table, "throttled")
	case streamingRetry:
		return n.retry(table, "transient")
	case streamingUnavailable:
		n.cooldowns.start(table, time.Now().Add(rt.Cooldown))
		return n.fallback(table, failure.String(), bytes.NewReader(buffered))
	default:
		return n.fallback(table, failure.String(), bytes.NewReader(buffered))
	}
}

func (n *uploader) fallback(table, reason string, data io.Reader) (streamOutcome, io.Reader) {
	n.recordRealtime(table, "fallback_"+reason)
	if logger.IsDebug() {
		logger.Debugf("Realtime batch using queued ingestion db=%s table=%s reason=%s", n.database, table, reason)
	}
	return streamFallback, data
}

// retry records that a realtime batch will be retried with streaming ingestion.
func (n *uploader) retry(table, reason string) (streamOutcome, io.Reader) {
	n.recordRealtime(table, "retry_"+reason)
	return streamRetry, nil
}

func (n *uploader) recordRealtime(table, outcome string) {
	metrics.IngestorRealtimeBatches.WithLabelValues(n.database, table, outcome).Inc()
}

// oldestSegment returns the creation time of the oldest segment in the batch.
func oldestSegment(batch *cluster.Batch) time.Time {
	var oldest time.Time
	for _, si := range batch.Segments {
		if oldest.IsZero() || si.CreatedAt.Before(oldest) {
			oldest = si.CreatedAt
		}
	}
	return oldest
}
