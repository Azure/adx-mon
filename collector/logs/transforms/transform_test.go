package transforms

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTransform(t *testing.T) {
	type testcase struct {
		name          string
		transformType string
		config        map[string]interface{}
		expectErr     bool
	}

	testcases := []testcase{
		{
			name:          "plugin_valid_config",
			transformType: "plugin",
			config: map[string]interface{}{
				"GoPath":     "plugin/test-plugin",
				"ImportName": "github.com/Azure/testplugin/pkg/transforms",
			},
			expectErr: false,
		},
		{
			name:          "plugin_bad_config",
			transformType: "plugin",
			config: map[string]interface{}{
				"GoPath":     "plugin/foo/bar",
				"ImportName": "github.com/Azure/testplugin/pkg/transforms",
			},
			expectErr: true,
		},
		{
			name:          "unknown plugin",
			transformType: "fakeplugin",
			config:        map[string]interface{}{},
			expectErr:     true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTransform(tc.transformType, tc.config)
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
