package types

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopy(t *testing.T) {
	log := &Log{
		Timestamp:         1,
		ObservedTimestamp: 2,
		Body: map[string]any{
			"key": "value",
			"complicated": map[string]any{
				"hello": "world",
			},
		},
		Attributes: map[string]any{
			"destination": "first_destination",
			"k8s.pod.labels": map[string]string{
				"app": "myapp",
			},
		},
		Resource: map[string]any{
			"resource.key": "resource.value",
		},
	}

	copy := log.Copy()
	copy.Attributes["destination"] = "second_destination"

	require.Equal(t, "first_destination", log.Attributes["destination"])
	require.Equal(t, "myapp", log.Attributes["k8s.pod.labels"].(map[string]string)["app"])
	require.Equal(t, "second_destination", copy.Attributes["destination"])
	require.Equal(t, "myapp", copy.Attributes["k8s.pod.labels"].(map[string]string)["app"])
	require.Equal(t, "value", copy.Body["key"].(string))
	require.Equal(t, "world", copy.Body["complicated"].(map[string]any)["hello"])
	require.Equal(t, "resource.value", copy.Resource["resource.key"].(string))
}

func TestROLogInterface(t *testing.T) {
	l := &Log{
		Timestamp:         12345,
		ObservedTimestamp: 67890,
		Body: map[string]any{
			"bodyKey":    "bodyVal",
			"bodyKeyTwo": "bodyValTwo",
		},
		Attributes: map[string]any{
			"attrKey":    "attrVal",
			"attrKeyTwo": "attrValTwo",
		},
		Resource: map[string]any{
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
	require.Equal(t, vals, l.Attributes)

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
	require.Equal(t, vals, l.Body)

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
	require.Equal(t, vals, l.Resource)

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
	require.Equal(t, l.Timestamp, cp.Timestamp)
	require.Equal(t, l.ObservedTimestamp, cp.ObservedTimestamp)
	require.Equal(t, l.Attributes, cp.Attributes)
	require.Equal(t, l.Body, cp.Body)
	require.Equal(t, l.Resource, cp.Resource)

	l.Timestamp = 235325235
	l.ObservedTimestamp = 423423423
	l.Attributes["attrKey"] = "newVal"
	l.Body["bodyKey"] = "newVal"
	l.Resource["resKey"] = "newVal"
	require.NotEqual(t, l.Timestamp, cp.Timestamp)
	require.NotEqual(t, l.ObservedTimestamp, cp.ObservedTimestamp)
	require.NotEqual(t, l.Attributes, cp.Attributes)
	require.NotEqual(t, l.Body, cp.Body)
	require.NotEqual(t, l.Resource, cp.Resource)
}

func TestLogBatch(t *testing.T) {
	ackCalled := false
	batch := &LogBatch{
		Logs: []*Log{
			{Timestamp: 100},
			{Timestamp: 200},
		},
		Ack: func() {
			ackCalled = true
		},
	}

	require.Equal(t, 2, len(batch.Logs))

	// Test ForEach
	var timestamps []uint64
	batch.ForEach(func(l ROLog) {
		timestamps = append(timestamps, l.GetTimestamp())
	})
	require.Equal(t, []uint64{100, 200}, timestamps)

	batch.Ack()
	require.True(t, ackCalled)

	// Test Reset
	batch.Reset()
	require.Empty(t, batch.Logs)
}
