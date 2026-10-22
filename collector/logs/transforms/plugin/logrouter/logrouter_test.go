package logrouter

import (
	"context"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromConfigMap(t *testing.T) {
	transformer, err := FromConfigMap(map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, transformer)
	assert.Equal(t, "LogRouterTransform", transformer.Name())
}

func TestTransform_LogSources(t *testing.T) {
	tests := []struct {
		name      string
		log       types.LogLiteral
		wantDB    string
		wantTable string
	}{
		{
			name: "no annotations - unchanged",
			log: types.LogLiteral{
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "foo"},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
		{
			name: "single source match",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "mySource:DestDB:DestTable",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "mySource"},
			},
			wantDB:    "DestDB",
			wantTable: "DestTable",
		},
		{
			name: "multiple sources - second matches",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "srcA:DBA:TableA,srcB:DBB:TableB",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "srcB"},
			},
			wantDB:    "DBB",
			wantTable: "TableB",
		},
		{
			name: "source no match - unchanged",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "srcA:DBA:TableA",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "noMatch"},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
		{
			name: "malformed source entry - skipped",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "srcA:DBA",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "srcA"},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
		{
			name: "empty source body field",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "srcA:DBA:TableA",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
	}

	transform := NewTransform()
	require.NoError(t, transform.Open(context.Background()))
	defer transform.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.log.ToLog()
			batch := &types.LogBatch{Logs: []*types.Log{log}}

			result, err := transform.Transform(context.Background(), batch)
			require.NoError(t, err)
			require.Len(t, result.Logs, 1)

			gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
			gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
			assert.Equal(t, tt.wantDB, gotDB)
			assert.Equal(t, tt.wantTable, gotTable)
		})
	}
}

func TestTransform_LogKeys(t *testing.T) {
	tests := []struct {
		name      string
		log       types.LogLiteral
		wantDB    string
		wantTable string
	}{
		{
			name: "single key match",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "myKey:myVal:DestDB:DestTable",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"myKey": "myVal"},
			},
			wantDB:    "DestDB",
			wantTable: "DestTable",
		},
		{
			name: "multiple keys - second matches",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "k1:v1:DB1:T1,k2:v2:DB2:T2",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"k2": "v2"},
			},
			wantDB:    "DB2",
			wantTable: "T2",
		},
		{
			name: "key present but wrong value - unchanged",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "myKey:expected:DestDB:DestTable",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"myKey": "actual"},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
		{
			name: "malformed key entry - skipped",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "k1:v1",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"k1": "v1"},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
	}

	transform := NewTransform()
	require.NoError(t, transform.Open(context.Background()))
	defer transform.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.log.ToLog()
			batch := &types.LogBatch{Logs: []*types.Log{log}}

			result, err := transform.Transform(context.Background(), batch)
			require.NoError(t, err)
			require.Len(t, result.Logs, 1)

			gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
			gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
			assert.Equal(t, tt.wantDB, gotDB)
			assert.Equal(t, tt.wantTable, gotTable)
		})
	}
}

func TestTransform_NonStringBodyValues(t *testing.T) {
	tests := []struct {
		name      string
		log       types.LogLiteral
		wantDB    string
		wantTable string
	}{
		{
			name: "log-sources with bool source value",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "true:Logs:BoolTable",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": true},
			},
			wantDB:    "Logs",
			wantTable: "BoolTable",
		},
		{
			name: "log-keys with integer body value",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "code:500:Logs:Errors",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"code": 500},
			},
			wantDB:    "Logs",
			wantTable: "Errors",
		},
		{
			name: "log-keys with float body value",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "score:3.14:Logs:Scores",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"score": 3.14},
			},
			wantDB:    "Logs",
			wantTable: "Scores",
		},
		{
			name: "log-keys with nil body value - no match",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "missing:val:Logs:Table",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{},
			},
			wantDB:    "defaultDB",
			wantTable: "defaultTable",
		},
	}

	transform := NewTransform()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.log.ToLog()
			batch := &types.LogBatch{Logs: []*types.Log{log}}

			result, err := transform.Transform(context.Background(), batch)
			require.NoError(t, err)
			require.Len(t, result.Logs, 1)

			gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
			gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
			assert.Equal(t, tt.wantDB, gotDB)
			assert.Equal(t, tt.wantTable, gotTable)
		})
	}
}

func TestTransform_LogSourcesTakesPrecedence(t *testing.T) {
	// When both log-sources and log-keys match, log-sources wins.
	log := types.LogLiteral{
		Resource: map[string]interface{}{
			ResourceLogSources: "src:SourceDB:SourceTable",
			ResourceLogKeys:    "myKey:myVal:KeyDB:KeyTable",
		},
		Attributes: map[string]interface{}{
			types.AttributeDatabaseName: "defaultDB",
			types.AttributeTableName:    "defaultTable",
		},
		Body: map[string]any{
			"source": "src",
			"myKey":  "myVal",
		},
	}

	transform := NewTransform()
	result, err := transform.Transform(context.Background(), &types.LogBatch{Logs: []*types.Log{log.ToLog()}})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)

	gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
	gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
	assert.Equal(t, "SourceDB", gotDB)
	assert.Equal(t, "SourceTable", gotTable)
}

func TestTransform_FallsThroughToLogKeys(t *testing.T) {
	// When log-sources doesn't match, log-keys is evaluated.
	log := types.LogLiteral{
		Resource: map[string]interface{}{
			ResourceLogSources: "noMatch:SourceDB:SourceTable",
			ResourceLogKeys:    "myKey:myVal:KeyDB:KeyTable",
		},
		Attributes: map[string]interface{}{
			types.AttributeDatabaseName: "defaultDB",
			types.AttributeTableName:    "defaultTable",
		},
		Body: map[string]any{
			"source": "other",
			"myKey":  "myVal",
		},
	}

	transform := NewTransform()
	result, err := transform.Transform(context.Background(), &types.LogBatch{Logs: []*types.Log{log.ToLog()}})
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)

	gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
	gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
	assert.Equal(t, "KeyDB", gotDB)
	assert.Equal(t, "KeyTable", gotTable)
}

func TestTransform_LogSourcesWhitespaceTrimming(t *testing.T) {
	tests := []struct {
		name      string
		log       types.LogLiteral
		wantDB    string
		wantTable string
	}{
		{
			name: "spaces after commas",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: "srcA:DBA:TableA, srcB:DBB:TableB",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "srcB"},
			},
			wantDB:    "DBB",
			wantTable: "TableB",
		},
		{
			name: "spaces around colons",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogSources: " srcA : DBA : TableA ",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"source": "srcA"},
			},
			wantDB:    "DBA",
			wantTable: "TableA",
		},
	}

	transform := NewTransform()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.log.ToLog()
			batch := &types.LogBatch{Logs: []*types.Log{log}}

			result, err := transform.Transform(context.Background(), batch)
			require.NoError(t, err)
			require.Len(t, result.Logs, 1)

			gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
			gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
			assert.Equal(t, tt.wantDB, gotDB)
			assert.Equal(t, tt.wantTable, gotTable)
		})
	}
}

func TestTransform_LogKeysWhitespaceTrimming(t *testing.T) {
	tests := []struct {
		name      string
		log       types.LogLiteral
		wantDB    string
		wantTable string
	}{
		{
			name: "spaces after commas",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: "k1:v1:DB1:T1, k2:v2:DB2:T2",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"k2": "v2"},
			},
			wantDB:    "DB2",
			wantTable: "T2",
		},
		{
			name: "spaces around colons",
			log: types.LogLiteral{
				Resource: map[string]interface{}{
					ResourceLogKeys: " myKey : myVal : DestDB : DestTable ",
				},
				Attributes: map[string]interface{}{
					types.AttributeDatabaseName: "defaultDB",
					types.AttributeTableName:    "defaultTable",
				},
				Body: map[string]any{"myKey": "myVal"},
			},
			wantDB:    "DestDB",
			wantTable: "DestTable",
		},
	}

	transform := NewTransform()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := tt.log.ToLog()
			batch := &types.LogBatch{Logs: []*types.Log{log}}

			result, err := transform.Transform(context.Background(), batch)
			require.NoError(t, err)
			require.Len(t, result.Logs, 1)

			gotDB := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeDatabaseName))
			gotTable := types.StringOrEmpty(result.Logs[0].GetAttributeValue(types.AttributeTableName))
			assert.Equal(t, tt.wantDB, gotDB)
			assert.Equal(t, tt.wantTable, gotTable)
		})
	}
}

func TestTransform_NilLog(t *testing.T) {
	transform := NewTransform()
	batch := &types.LogBatch{Logs: []*types.Log{nil}}
	result, err := transform.Transform(context.Background(), batch)
	require.NoError(t, err)
	require.Len(t, result.Logs, 1)
}
