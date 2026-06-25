package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/alerter/rules"
	azkustodata "github.com/Azure/azure-kusto-go/azkustodata"
	"github.com/Azure/azure-kusto-go/azkustodata/kql"
)

type QueryContext struct {
	Rule      *rules.Rule
	Query     string
	Stmt      azkustodata.Statement
	Params    *kql.Parameters
	Region    string
	StartTime time.Time
	EndTime   time.Time
}

type queryParam struct {
	name  string
	value any
}

func NewQueryContext(rule *rules.Rule, endTime time.Time, region string) (*QueryContext, error) {
	qc := &QueryContext{
		Rule:      rule,
		Region:    region,
		StartTime: endTime.Add(-rule.Interval),
		EndTime:   endTime,
	}
	err := qc.wrapStmt()
	return qc, err
}

func (q *QueryContext) wrapStmt() error {
	q.Query = q.Rule.Query
	q.Stmt = q.Rule.Stmt

	if q.Rule.IsMgmtQuery {
		return nil
	}

	query := fmt.Sprintf(`
let _startTime = _adxmonStartTime;
let _endTime = _adxmonEndTime;
let _region = _adxmonRegion;
%s
`, strings.TrimSpace(q.Rule.Query))

	stmt := kql.New("").AddUnsafe(query)
	queryParams := []queryParam{
		{name: "_adxmonStartTime", value: q.StartTime},
		{name: "_adxmonEndTime", value: q.EndTime},
		{name: "_adxmonRegion", value: q.Region},
		{name: "ParamRegion", value: q.Region}, // This is a deprecated parameter.
	}
	params := kql.NewParameters()
	for _, param := range queryParams {
		var literal string
		switch v := param.value.(type) {
		case string:
			params.AddString(param.name, v)
			literal = fmt.Sprintf("%q", v)
		case time.Time:
			params.AddDateTime(param.name, v)
			literal = fmt.Sprintf("datetime(%s)", v.Format("2006-01-02T15:04:05.999999Z"))
		default:
			return fmt.Errorf("unsupported query parameter %s type %T", param.name, v)
		}
		query = strings.ReplaceAll(query, param.name, literal)
	}

	q.Query = query
	q.Params = params
	q.Stmt = stmt

	return nil
}
