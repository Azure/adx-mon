package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogPoolReset(t *testing.T) {
	log := LogPool.Get()
	log.SetTimestamp(123)
	log.SetObservedTimestamp(456)
	log.SetBodyValue("message", "payload")
	log.SetAttributeValue("attribute", []byte("payload"))
	log.SetResourceValue("resource", map[string]any{"nested": "payload"})
	log.Freeze()
	body, attributes, resource := log.body, log.attributes, log.resource
	LogPool.Put(log)

	// Payloads are released on return, not on the next checkout. Do not rely
	// on sync.Pool returning this specific log from a later Get.
	require.Empty(t, body)
	require.Empty(t, attributes)
	require.Empty(t, resource)

	log = LogPool.Get()
	require.Zero(t, log.GetTimestamp())
	require.Zero(t, log.GetObservedTimestamp())
	require.Zero(t, log.BodyLen())
	require.Zero(t, log.AttributeLen())
	require.Zero(t, log.ResourceLen())
	require.Zero(t, log.frozen)
	log.SetBodyValue("new", "message")
	log.SetAttributeValue("new", "attribute")
	log.SetResourceValue("new", "resource")
	LogPool.Put(log)
}

func TestLogBatchPoolReset(t *testing.T) {
	batch := LogBatchPool.Get()
	require.Empty(t, batch.Logs)
	require.NotNil(t, batch.Ack)
	require.NotPanics(t, batch.Ack)

	ackCalled := false
	batch.Ack = func() { ackCalled = true }
	log := LogPool.Get()
	log.SetBodyValue("message", "payload")
	body := log.body
	batch.Logs = append(batch.Logs, log)
	backing := batch.Logs
	DisposeLogBatch(batch)

	require.Empty(t, body)
	require.Nil(t, backing[0])
	require.False(t, ackCalled, "disposing a batch must not acknowledge it")

	batch = LogBatchPool.Get()
	require.Empty(t, batch.Logs)
	require.NotNil(t, batch.Ack)
	batch.Ack()
	require.False(t, ackCalled, "a pooled batch must not retain its old acknowledgement")
	LogBatchPool.Put(batch)
}
