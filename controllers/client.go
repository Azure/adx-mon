package controllers

import (
	"context"

	"github.com/Azure/azure-kusto-go/kusto/data/table"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
)

type Client interface {
	Query(ctx context.Context, r adxmonv1.AlertRule, fn func(endpoint string, rule adxmonv1.AlertRule, row *table.Row) error) error
}
