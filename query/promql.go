package query

import (
	"github.com/Azure/adx-mon/transform"
	"github.com/davecgh/go-spew/spew"
	"github.com/prometheus/prometheus/promql/parser"
	"strings"
	"time"
)

const timeFormat = "2006-01-02T15:04:05.999Z"

type KQLTranspiler struct {
	table    string
	filter   []string
	from, to time.Time
	interval time.Duration
	groupBy  []string
	call     string

	statements []Statement

	stack stack
}

func (v *KQLTranspiler) Walk(node parser.Node) error {
	if node == nil {
		return nil
	}

	switch t := node.(type) {
	case *parser.AggregateExpr:
		stmt, err := v.buildAgregationExpr(t)
		if err != nil {
			return err
		}
		v.statements = append(v.statements, stmt)

	case *parser.BinaryExpr:
		lhs, err := v.buildAgregationExpr(t.LHS.(*parser.AggregateExpr))
		if err != nil {
			return err
		}

		rhs, err := v.buildAgregationExpr(t.RHS.(*parser.AggregateExpr))
		if err != nil {
			return err
		}

		println(lhs.String())
		println(rhs.String())

	default:
		spew.Dump(node)
	}
	return nil
}

func (v *KQLTranspiler) String() string {
	var sb strings.Builder
	for _, v := range v.statements {
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

func (v *KQLTranspiler) buildTableExpr(t *parser.MatrixSelector) (*Table, error) {
	to := time.Now().UTC()
	from := to.Add(-t.Range)
	v.to = to
	v.from = from

	vs := t.VectorSelector.(*parser.VectorSelector)

	table, err := v.buildTable(vs)
	if err != nil {
		return nil, err
	}
	// Add time filter as first where clause
	table.Where = append([]Expression{Where{BetweenExpr{From: from, To: to}}}, table.Where...)
	return table, nil
}

func (v *KQLTranspiler) buildTable(vs *parser.VectorSelector) (*Table, error) {
	v.table = string(transform.Normalize([]byte(vs.Name)))

	table := &Table{Name: string(transform.Normalize([]byte(vs.Name)))}

	for _, vv := range vs.LabelMatchers {
		if vv.Name == "__name__" {
			continue
		}

		table.Where = append(table.Where, Where{BinaryExpr{vv.Name, vv.Type.String(), vv.Value}})
	}

	return table, nil
}

func (v *KQLTranspiler) buildInvokeFunc(t *parser.Call) (*Call, error) {
	switch t.Func.Name {
	case "irate":
		args, err := v.buildInvokeArgs(t.Args)
		if err != nil {
			return nil, err
		}

		return &Call{
			Func:    "series_irate",
			GroupBy: nil,
			Args:    args,
		}, nil

	default:
		args, err := v.buildInvokeArgs(t.Args)
		if err != nil {
			return nil, err
		}

		return &Call{
			Func:    "series_rate",
			GroupBy: nil,
			Args:    args,
		}, nil

	}
}

func (v *KQLTranspiler) buildInvokeArgs(args parser.Expressions) ([]Expression, error) {
	for _, arg := range args {
		switch t := arg.(type) {
		case *parser.MatrixSelector:
			tbl, err := v.buildTableExpr(t)
			if err != nil {
				return nil, err
			}
			v.statements = append(v.statements, tbl)

		default:
			panic(t.String())
		}
	}
	return nil, nil
}

func (v *KQLTranspiler) buildAgregationExpr(t *parser.AggregateExpr) (Statement, error) {
	groupBy := t.Grouping
	switch s := t.Expr.(type) {
	case *parser.Call:
		stmt, err := v.buildInvokeFunc(s)
		if err != nil {
			return nil, err
		}
		stmt.GroupBy = groupBy
		return stmt, nil
	case *parser.VectorSelector:
		tbl, err := v.buildTable(s)
		if err != nil {
			return nil, err
		}
		return tbl, nil
	default:
		panic(s.String())
	}
}
