package plugin

import (
	"fmt"
	"path/filepath"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

/* Plugins are defined with a Go package that exports a New() function that returns a types.Transformer.
   The structure of a plugin is like the following, simulating a full GoPath with a single project within.

   The plugin system does not support Go modules, so all dependencies must be vendored and the plugin path
   must be the full package path.
.
└── src
    └── github.com
        └── Azure
            └── testplugin
                ├── go.mod
                ├── pkg
                │   ├── transforms
                │   │   ├── addemoji.go
                │   │   └── addemoji_test.go
                │   └── mapping
                │       └── emoji_mapping.go
                └── vendor
                    └── github.com
                        └── Azure
                            └── adx-mon
                                └── collector
                                    └── logs
                                        └── types
                                            └── processors.go
                                            └── logs.go
*/

func FromConfigMap(config map[string]interface{}) (types.Transformer, error) {
	goPath, ok := config["GoPath"].(string)
	if !ok {
		return nil, fmt.Errorf("GoPath is required")
	}

	importName, ok := config["ImportName"].(string)
	if !ok {
		return nil, fmt.Errorf("ImportName is required")
	}

	transformConfig := TransformConfig{
		GoPath:     goPath,
		ImportName: importName,
	}

	return NewTransform(transformConfig)
}

type TransformConfig struct {
	// Path on disk that contains the plugin.
	// This expects a directory with a src directory at the base.
	GoPath string

	// Import package of the plugin that contains the New() function.
	// e.g. github.com/Azure/emojitransforms/pkg/transforms
	ImportName string
}

func NewTransform(config TransformConfig) (types.Transformer, error) {
	baseName := filepath.Base(config.ImportName)

	i := interp.New(interp.Options{GoPath: config.GoPath})
	// Go stdlib symbols
	i.Use(stdlib.Symbols)
	// Collector plugin symbols
	i.Use(Symbols)

	_, err := i.Eval(fmt.Sprintf(`import "%s"`, config.ImportName))
	if err != nil {
		return nil, fmt.Errorf("failed to import %s: %w", config.ImportName, err)
	}

	newFuncInterface, err := i.Eval(fmt.Sprintf("%s.New", baseName))
	if err != nil {
		return nil, fmt.Errorf("failed to get New function from %s: %w", baseName, err)
	}
	// Consider changing this interface to accept a configuration object that can contain metrics, etc.
	results := newFuncInterface.Call(nil)

	var ok bool
	transformer, ok := results[0].Interface().(types.Transformer)
	if !ok {
		return nil, fmt.Errorf("failed to cast %s.New() result to *logs.Transformer", baseName)
	}

	return transformer, nil
}
