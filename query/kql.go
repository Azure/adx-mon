package query

import (
	"fmt"
	"strings"
	"time"
)

type Statement interface {
	fmt.Stringer
}

type Table struct {
	Name  string
	Where []Expression
}

func (t Table) String() string {
	var sb strings.Builder
	sb.WriteString(t.Name)
	sb.WriteString("\n")
	for i, v := range t.Where {
		sb.WriteString(v.String())
		if i < len(t.Where)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

type Where struct {
	Expr Expression
}

func (w Where) String() string {
	return fmt.Sprintf("| where %s", w.Expr.String())
}

type BetweenExpr struct {
	From time.Time
	To   time.Time
}

func (b BetweenExpr) String() string {
	var sb strings.Builder
	sb.WriteString("Timestamp >= datetime(")
	sb.WriteString(b.From.Format(timeFormat))
	sb.WriteString(") and Timestamp < datetime(")
	sb.WriteString(b.To.Format(timeFormat))
	sb.WriteString(")")
	return sb.String()
}

type BinaryExpr struct {
	Left, Op, Right string
}

func (b BinaryExpr) String() string {
	return fmt.Sprintf("Labels.%s %s %q", b.Left, b.Op, b.Right)
}

type Call struct {
	Func    string
	GroupBy []string
	Args    []Expression
}

func (c Call) String() string {
	var sb strings.Builder
	sb.WriteString("| invoke ")
	sb.WriteString(c.Func)
	sb.WriteByte('(')
	if len(c.GroupBy) > 0 {
		sb.WriteString("groupBy=dynamic([")
		for i, gb := range c.GroupBy {
			sb.WriteString("'")
			sb.WriteString(gb)
			sb.WriteString("'")
			if i < len(c.GroupBy)-1 {
				sb.WriteByte(',')
			}
		}
		sb.WriteString("])")
	}
	sb.WriteByte(')')
	return sb.String()
}

type Expression interface {
	fmt.Stringer
}
