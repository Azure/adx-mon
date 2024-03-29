package types

import (
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
