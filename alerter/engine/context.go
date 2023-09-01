package engine

import (
	"fmt"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/azure-kusto-go/kusto"
	kustotypes "github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"strings"
	"time"
)

type QueryContext struct {
	Rule      *rules.Rule
	Query     string
	Stmt      kusto.Stmt
	Params    kusto.Parameters
	Region    string
	StartTime time.Time
	EndTime   time.Time
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

	stmt := kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(query)

	// Setup the query execution parameters that can be reference in the query
	var err error
	def := kusto.NewDefinitions()
	def, err = def.With(kusto.ParamTypes{
		"_adxmonStartTime": kusto.ParamType{Type: kustotypes.DateTime},
		"_adxmonEndTime":   kusto.ParamType{Type: kustotypes.DateTime},
		"_adxmonRegion":    kusto.ParamType{Type: kustotypes.String},
		"ParamRegion":      kusto.ParamType{Type: kustotypes.String}, // This is a deprecated parameter
	})
	if err != nil {
		return fmt.Errorf("failed to create query definitions: %w", err)
	}

	stmt, err = stmt.WithDefinitions(def)
	if err != nil {
		return fmt.Errorf("failed to create query statement: %w", err)
	}

	qv := kusto.QueryValues{}
	qv["_adxmonStartTime"] = q.StartTime
	qv["_adxmonEndTime"] = q.EndTime
	qv["_adxmonRegion"] = q.Region
	qv["ParamRegion"] = q.Region

	for k, v := range qv {
		switch vv := v.(type) {
		case string:
			query = strings.Replace(query, k, fmt.Sprintf("\"%s\"", vv), -1)
		case time.Time:
			query = strings.Replace(query, k, fmt.Sprintf("datetime(%s)", vv.Format("2006-01-02T15:04:05.999999Z")), -1)
		default:
			panic(fmt.Sprintf("unimplemented query type: %v", vv))
		}
	}

	params, err := kusto.NewParameters().With(qv)
	if err != nil {
		return fmt.Errorf("failed to create kusto parameters: %w", err)
	}

	stmt, err = stmt.WithParameters(params)
	if err != nil {
		return fmt.Errorf("failed to create kusto statement: %w", err)
	}

	q.Query = query
	q.Params = params
	q.Stmt = stmt

	return nil
}
