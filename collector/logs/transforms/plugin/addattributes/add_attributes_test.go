package addattributes

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddAttributesTransform_New(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		expectedAttrs map[string]string
	}{
		{
			name: "with values",
			config: Config{
				ResourceValues: map[string]string{
					"environment": "test",
					"service":     "adx-mon",
				},
			},
			expectedAttrs: map[string]string{
				"environment": "test",
				"service":     "adx-mon",
			},
		},
		{
			name: "empty config",
			config: Config{
				ResourceValues: map[string]string{},
			},
			expectedAttrs: map[string]string{},
		},
		{
			name: "with dynamic labeler",
			config: Config{
				ResourceValues: map[string]string{
					"existing": "value",
				},
				DynamicLabeler: &stubLogLabeler{},
			},
			expectedAttrs: map[string]string{
				"existing": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := NewTransform(tt.config)
			assert.Equal(t, tt.expectedAttrs, transformer.resourceValues)
			assert.Equal(t, tt.config.DynamicLabeler, transformer.dynamicLabeler)
		})
	}
}

func TestAddAttributesTransform_FromConfigMap(t *testing.T) {
	tests := []struct {
		name        string
		configMap   map[string]interface{}
		expectError bool
	}{
		{
			name: "valid config",
			configMap: map[string]interface{}{
				"ResourceValues": map[string]string{
					"environment": "test",
					"service":     "adx-mon",
				},
			},
			expectError: false,
		},
		{
			name: "missing resource_values",
			configMap: map[string]interface{}{
				"wrong_key": "value",
			},
			expectError: true,
		},
		{
			name: "resource_values not a map",
			configMap: map[string]interface{}{
				"ResourceValues": "not a map",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer, err := FromConfigMap(tt.configMap)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, transformer)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, transformer)
				assert.IsType(t, &Transform{}, transformer)
			}
		})
	}
}

func TestAddAttributesTransform_Transform(t *testing.T) {
	tests := []struct {
		name              string
		resourceValues    map[string]string
		inputResources    map[string]interface{}
		expectedResources map[string]interface{}
	}{
		{
			name: "add new attributes",
			resourceValues: map[string]string{
				"environment": "test",
				"service":     "adx-mon",
			},
			inputResources: map[string]interface{}{
				"existing": "value",
			},
			expectedResources: map[string]interface{}{
				"existing":    "value",
				"environment": "test",
				"service":     "adx-mon",
			},
		},
		{
			name: "override existing attributes",
			resourceValues: map[string]string{
				"existing": "new-value",
			},
			inputResources: map[string]interface{}{
				"existing": "old-value",
			},
			expectedResources: map[string]interface{}{
				"existing": "new-value",
			},
		},
		{
			name:           "no attributes to add",
			resourceValues: map[string]string{},
			inputResources: map[string]interface{}{
				"existing": "value",
			},
			expectedResources: map[string]interface{}{
				"existing": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := &Transform{
				resourceValues: tt.resourceValues,
			}

			err := transformer.Open(context.Background())
			require.NoError(t, err)
			defer func() {
				err := transformer.Close()
				require.NoError(t, err)
			}()

			// Create a log batch with one log
			log := types.NewLog()
			for k, v := range tt.inputResources {
				log.SetResourceValue(k, v)
			}

			batch := &types.LogBatch{
				Logs: []*types.Log{log},
			}

			// Transform the batch
			ctx := context.Background()
			resultBatch, err := transformer.Transform(ctx, batch)
			require.NoError(t, err)
			require.Equal(t, 1, len(resultBatch.Logs))

			// Check that the resources match the expected values
			for k, expectedVal := range tt.expectedResources {
				val, ok := resultBatch.Logs[0].GetResourceValue(k)
				require.True(t, ok, "Expected resource key %s not found", k)
				require.Equal(t, expectedVal, val)
			}
		})
	}
}

func TestAddAttributesTransform_Name(t *testing.T) {
	transformer := &Transform{}
	assert.Equal(t, "AddAttributesTransform", transformer.Name())
}

func TestAddAttributesTransform_TransformWithDynamicLabeler(t *testing.T) {
	labeler := &stubLogLabeler{
		values: map[string]string{
			"dynamic": "value",
			"overlap": "from-labeler",
		},
	}

	transformer := NewTransform(Config{
		ResourceValues: map[string]string{
			"static":  "static-value",
			"overlap": "from-static",
		},
		DynamicLabeler: labeler,
	})

	logs := []*types.Log{types.NewLog(), types.NewLog()}
	batch := &types.LogBatch{Logs: logs}

	_, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)

	require.Equal(t, len(logs), labeler.callCount)

	for _, log := range logs {
		val, ok := log.GetResourceValue("dynamic")
		require.True(t, ok)
		require.Equal(t, "value", val)

		val, ok = log.GetResourceValue("static")
		require.True(t, ok)
		require.Equal(t, "static-value", val)

		val, ok = log.GetResourceValue("overlap")
		require.True(t, ok)
		require.Equal(t, "from-static", val)
	}
}

func TestAddAttributesTransform_TransformWithDynamicLabelerOnly(t *testing.T) {
	labeler := &stubLogLabeler{
		values: map[string]string{
			"dynamic": "value",
		},
	}

	transformer := NewTransform(Config{
		ResourceValues: nil,
		DynamicLabeler: labeler,
	})

	log := types.NewLog()
	batch := &types.LogBatch{Logs: []*types.Log{log}}

	_, err := transformer.Transform(context.Background(), batch)
	require.NoError(t, err)

	require.Equal(t, 1, labeler.callCount)

	val, ok := log.GetResourceValue("dynamic")
	require.True(t, ok)
	require.Equal(t, "value", val)
}

type stubLogLabeler struct {
	values    map[string]string
	callCount int
}

func (s *stubLogLabeler) SetResourceValues(log *types.Log) {
	s.callCount++
	for k, v := range s.values {
		log.SetResourceValue(k, v)
	}
}
