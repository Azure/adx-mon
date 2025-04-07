package types

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopy(t *testing.T) {
	log := &Log{
		timestamp:         1,
		observedTimestamp: 2,
		body: map[string]any{
			"key": "value",
			"complicated": map[string]any{
				"hello": "world",
			},
		},
		attributes: map[string]any{
			"destination": "first_destination",
			"k8s.pod.labels": map[string]string{
				"app": "myapp",
			},
		},
		resource: map[string]any{
			"resource.key": "resource.value",
		},
	}

	copy := log.Copy()
	copy.attributes["destination"] = "second_destination"

	require.Equal(t, "first_destination", log.attributes["destination"])
	require.Equal(t, "myapp", log.attributes["k8s.pod.labels"].(map[string]string)["app"])
	require.Equal(t, "second_destination", copy.attributes["destination"])
	require.Equal(t, "myapp", copy.attributes["k8s.pod.labels"].(map[string]string)["app"])
	require.Equal(t, "value", copy.body["key"].(string))
	require.Equal(t, "world", copy.body["complicated"].(map[string]any)["hello"])
	require.Equal(t, "resource.value", copy.resource["resource.key"].(string))
}

func TestROLogInterface(t *testing.T) {
	l := &Log{
		timestamp:         12345,
		observedTimestamp: 67890,
		body: map[string]any{
			"bodyKey":    "bodyVal",
			"bodyKeyTwo": "bodyValTwo",
		},
		attributes: map[string]any{
			"attrKey":    "attrVal",
			"attrKeyTwo": "attrValTwo",
		},
		resource: map[string]any{
			"resKey":    "resVal",
			"resKeyTwo": false,
		},
	}

	// Check basic getters
	require.Equal(t, uint64(12345), l.GetTimestamp())
	require.Equal(t, uint64(67890), l.GetObservedTimestamp())

	val, ok := l.GetBodyValue("bodyKey")
	require.True(t, ok)
	require.Equal(t, "bodyVal", val)

	val, ok = l.GetAttributeValue("attrKey")
	require.True(t, ok)
	require.Equal(t, "attrVal", val)

	val, ok = l.GetResourceValue("resKey")
	require.True(t, ok)
	require.Equal(t, "resVal", val)

	// Check iteration
	vals := make(map[string]any)
	err := l.ForEachAttribute(func(k string, v any) error {
		vals[k] = v
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, vals, l.attributes)

	err = l.ForEachAttribute(func(k string, v any) error {
		return errors.New("test")
	})
	require.Error(t, err)

	clear(vals)
	err = l.ForEachBody(func(k string, v any) error {
		vals[k] = v
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, vals, l.body)

	err = l.ForEachBody(func(k string, v any) error {
		return errors.New("test")
	})
	require.Error(t, err)

	clear(vals)
	err = l.ForEachResource(func(k string, v any) error {
		vals[k] = v
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, vals, l.resource)

	err = l.ForEachResource(func(k string, v any) error {
		return errors.New("test")
	})

	require.Error(t, err)
	require.Equal(t, 2, l.AttributeLen())
	require.Equal(t, 2, l.BodyLen())
	require.Equal(t, 2, l.ResourceLen())

	// Check Copy
	cp := l.Copy()
	require.NotSame(t, l, cp)
	require.Equal(t, l.timestamp, cp.timestamp)
	require.Equal(t, l.observedTimestamp, cp.observedTimestamp)
	require.Equal(t, l.attributes, cp.attributes)
	require.Equal(t, l.body, cp.body)
	require.Equal(t, l.resource, cp.resource)

	l.timestamp = 235325235
	l.observedTimestamp = 423423423
	l.attributes["attrKey"] = "newVal"
	l.body["bodyKey"] = "newVal"
	l.resource["resKey"] = "newVal"
	require.NotEqual(t, l.timestamp, cp.timestamp)
	require.NotEqual(t, l.observedTimestamp, cp.observedTimestamp)
	require.NotEqual(t, l.attributes, cp.attributes)
	require.NotEqual(t, l.body, cp.body)
	require.NotEqual(t, l.resource, cp.resource)
}

func TestLogBatch(t *testing.T) {
	ackCalled := false
	batch := &LogBatch{
		Logs: []*Log{
			{timestamp: 100},
			{timestamp: 200},
		},
		Ack: func() {
			ackCalled = true
		},
	}

	require.Equal(t, 2, len(batch.Logs))

	batch.Ack()
	require.True(t, ackCalled)

	// Test Reset
	batch.Reset()
	require.Empty(t, batch.Logs)
}

func BenchmarkLogWrites(b *testing.B) {
	log := LogPool.Get(1).(*Log)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		log.Reset()
		log.SetTimestamp(12345)
		log.SetObservedTimestamp(67890)
		log.SetBodyValue("key", "value")
		log.SetAttributeValue("attr", "value")
		log.SetResourceValue("res", "value")
		log.SetBodyValue("complicated", map[string]any{
			"hello": "world",
		})
		log.Freeze()
	}
}
