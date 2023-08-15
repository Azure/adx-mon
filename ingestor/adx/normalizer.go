package adx

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
)

type querier interface {
	Query(ctx context.Context, db string, query kusto.Stmt, options ...kusto.QueryOption) (*kusto.RowIterator, error)
}

type Normalizer struct {
	database string
	KustoCli querier

	mu     sync.RWMutex
	tables []string
}

func (n *Normalizer) Open(ctx context.Context) error {
	return nil
}

func (n *Normalizer) Close() error {
	return nil
}

func (n *Normalizer) AddTable(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.tables = append(n.tables, name)
}

func (n *Normalizer) rollup() {
	// for _, t := range n.tables {
	// 	maxRollup, err := n.findMaxRollup(t)
	// }
}

func (n *Normalizer) findMaxRollup(ctx context.Context, table string) (time.Time, error) {
	var sb strings.Builder
	sb.WriteString(table)
	sb.WriteString("\n| summarize max(TimeStamp)")
	// showStmt := kusto.NewStmt("", kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(sb.String())

	// iter, err := n.KustoCli.Query(ctx, n.database, showStmt)

	return time.Time{}, nil
}
