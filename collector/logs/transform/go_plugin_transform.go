package transform

import (
	"context"
	"fmt"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/plugins"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

type GoPlugin struct {
	ImportName string
	BaseName   string

	transformer logs.Transformer
}

func NewGoPlugin(importName, baseName string) (*GoPlugin, error) {
	plugin := &GoPlugin{
		ImportName: importName,
		BaseName:   baseName,
	}

	i := interp.New(interp.Options{GoPath: "./local_plugins"})
	i.Use(stdlib.Symbols)

	// TODO - types should be in a new package probably to make vendoring easier.
	i.Use(plugins.Symbols)

	_, err := i.Eval(fmt.Sprintf(`import "%s"`, importName))
	if err != nil {
		return nil, fmt.Errorf("failed to import %s: %w", importName, err)
	}

	// TODO - interface for New should include some kind of config object
	// To pass in metrics, etc.
	newFuncInterface, err := i.Eval(fmt.Sprintf("%s.New", baseName))
	if err != nil {
		return nil, fmt.Errorf("failed to get New function from %s: %w", baseName, err)
	}
	results := newFuncInterface.Call(nil)

	var ok bool
	plugin.transformer, ok = results[0].Interface().(logs.Transformer)
	if !ok {
		return nil, fmt.Errorf("failed to cast %s.New() result to logs.Transformer", baseName)
	}

	return plugin, nil
}

func (t *GoPlugin) Open(ctx context.Context) error {
	return t.transformer.Open(ctx)
}

func (t *GoPlugin) Transform(ctx context.Context, batch *logs.LogBatch) (*logs.LogBatch, error) {
	return t.transformer.Transform(ctx, batch)
}

func (t *GoPlugin) Close() error {
	return t.transformer.Close()
}
