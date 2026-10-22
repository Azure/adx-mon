package clickhouse

import "testing"

func TestMapClickHouseType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		columnName string
		adxType    string
		expected   string
	}{
		{
			name:       "SeriesId overrides numeric type",
			columnName: "SeriesId",
			adxType:    "long",
			expected:   "UInt64",
		},
		{
			name:       "SeverityNumber overrides numeric type",
			columnName: "SeverityNumber",
			adxType:    "int",
			expected:   "Int32",
		},
		{
			name:       "Dynamic falls back to string",
			columnName: "Labels",
			adxType:    "dynamic",
			expected:   "String",
		},
		{
			name:       "Default string mapping",
			columnName: "Body",
			adxType:    "string",
			expected:   "String",
		},
		{
			name:       "Unknown type defaults to string",
			columnName: "Attributes",
			adxType:    "custom",
			expected:   "String",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := mapClickHouseType(tc.columnName, tc.adxType)
			if got != tc.expected {
				t.Fatalf("mapClickHouseType(%q, %q) = %q, want %q", tc.columnName, tc.adxType, got, tc.expected)
			}
		})
	}
}
