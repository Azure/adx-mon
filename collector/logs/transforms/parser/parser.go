package parser

import "github.com/Azure/adx-mon/collector/logs/types"

type Parser interface {
	Parse(*types.Log) error
}
