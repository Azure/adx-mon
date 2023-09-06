// package plugins provides wrappers for types shared with plugins
package plugins

import "reflect"

// Symbols maps packages -> exported symbols -> reflect.Value for the symbol
// Consumed by yaegi's interpreter for mapping shared types
var Symbols = map[string]map[string]reflect.Value{}

//go:generate yaegi extract -name plugins github.com/Azure/adx-mon/collector/logs
