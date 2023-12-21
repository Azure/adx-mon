package plugin

import "reflect"

// Symbols maps packages -> exported symbols -> reflect.Value for the symbol
// Consumed by yaegi's interpreter for mapping shared types
var Symbols = map[string]map[string]reflect.Value{}

// Generate exported types
//go:generate yaegi extract -name plugin github.com/Azure/adx-mon/collector/logs/types
