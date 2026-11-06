package parser

import (
	jsonv1 "encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func TestJsonParse(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedBody map[string]any
		expectErr    bool
	}{
		{name: "empty", expectErr: true},
		{name: "plain text", input: "not-a-string", expectErr: true},
		{name: "incomplete object", input: "{", expectErr: true},
		{name: "leading whitespace", input: ` {"a":1}`, expectErr: true},
		{name: "array", input: `[1,2]`, expectErr: true},
		{name: "null", input: `null`, expectErr: true},
		{name: "trailing value", input: `{"a":1} {}`, expectErr: true},
		{name: "trailing garbage", input: `{"a":1}x`, expectErr: true},
		{name: "trailing comma", input: `{"a":1,}`, expectErr: true},
		{name: "number overflow", input: `{"a":1e1000}`, expectErr: true},
		{name: "empty object", input: `{}`, expectedBody: map[string]any{}},
		{
			name:         "valid json",
			input:        `{"a":1,"b":"2","c":{"d":3}}`,
			expectedBody: map[string]any{"a": 1.0, "b": "2", "c": map[string]any{"d": 3.0}},
		},
		{
			name:         "duplicate scalar",
			input:        `{"a":1,"a":"last"}`,
			expectedBody: map[string]any{"a": "last"},
		},
		{
			name:         "duplicate object replaces rather than merges",
			input:        `{"a":{"first":1},"a":{"last":2}}`,
			expectedBody: map[string]any{"a": map[string]any{"last": 2.0}},
		},
		{
			name:         "nested duplicates",
			input:        `{"a":{"b":1,"b":null},"items":[{"x":true,"x":false}]}`,
			expectedBody: map[string]any{"a": map[string]any{"b": nil}, "items": []any{map[string]any{"x": false}}},
		},
		{
			name:         "invalid UTF-8",
			input:        "{\"key\xff\":\"value\xfe\"}",
			expectedBody: map[string]any{"key\ufffd": "value\ufffd"},
		},
		{
			name:         "unpaired surrogate",
			input:        `{"a":"\ud800"}`,
			expectedBody: map[string]any{"a": "\ufffd"},
		},
		{
			name:         "escaped key duplicates",
			input:        `{"a":1,"\u0061":2}`,
			expectedBody: map[string]any{"a": 2.0},
		},
		{
			name:         "case sensitive map keys",
			input:        `{"a":1,"A":2}`,
			expectedBody: map[string]any{"a": 1.0, "A": 2.0},
		},
		{
			name:         "value types and trailing whitespace",
			input:        "{\"number\":9007199254740993,\"bool\":true,\"null\":null,\"array\":[],\"object\":{}} \n\t",
			expectedBody: map[string]any{"number": float64(9007199254740993), "bool": true, "null": nil, "array": []any{}, "object": map[string]any{}},
		},
	}

	p, err := NewJsonParser(JsonParserConfig{})
	require.NoError(t, err)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := types.NewLog()
			err := p.Parse(log, tt.input)
			if tt.expectErr {
				require.ErrorIs(t, err, ErrNotJson)
				require.Empty(t, log.GetBody())
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expectedBody, log.GetBody())
			}
			require.Zero(t, p.reader.Size(), "parser must not retain the input string")
		})
	}
}

func TestJsonParseReuse(t *testing.T) {
	p, err := NewJsonParser(JsonParserConfig{})
	require.NoError(t, err)
	first := types.NewLog()
	require.NoError(t, p.Parse(first, `{"first":{"nested":1}}`))

	for _, msg := range []string{
		`{"partial":1,"bad":`,
		`{"partial":1} {}`,
		`{"partial":1,"overflow":1e1000}`,
	} {
		log := types.NewLog()
		log.SetBodyValue("stream", "stdout")
		ExecuteParsers([]Parser{p}, log, msg, "test")
		require.Equal(t, map[string]any{"stream": "stdout", types.BodyKeyMessage: msg}, log.GetBody())

		next := types.NewLog()
		require.NoError(t, p.Parse(next, `{"next":true}`))
		require.Equal(t, map[string]any{"next": true}, next.GetBody())
	}
	require.Equal(t, map[string]any{"first": map[string]any{"nested": 1.0}}, first.GetBody())
}

func FuzzJsonParseCompatibility(f *testing.F) {
	for _, msg := range []string{
		`{}`,
		`{"a":1,"b":"2","c":{"d":3}}`,
		`{"a":{"first":1},"a":{"last":2}}`,
		`{"a":null,"a":[1,true,{"b":false}]}`,
		"{\"key\xff\":\"value\xfe\"}",
		`{"a":"\ud800"}`,
		`{"a":1,"\u0061":2}`,
		`{"a":1e1000}`,
		`{"partial":1,"bad":`,
		`{"a":1} {}`,
		` {"a":1}`,
	} {
		f.Add(msg, `{"next":true}`)
	}
	f.Fuzz(func(t *testing.T, first, second string) {
		p, err := NewJsonParser(JsonParserConfig{})
		require.NoError(t, err)
		checkJsonParseCompatibility(t, p, first)
		checkJsonParseCompatibility(t, p, second)
	})
}

func checkJsonParseCompatibility(t *testing.T, p *JsonParser, msg string) {
	t.Helper()
	var want map[string]any
	wantErr := len(msg) == 0 || msg[0] != '{'
	if !wantErr {
		wantErr = jsonv1.Unmarshal([]byte(msg), &want) != nil
	}
	log := types.NewLog()
	log.SetBodyValue("stream", "stdout")
	err := p.Parse(log, msg)
	if wantErr {
		require.ErrorIs(t, err, ErrNotJson)
		require.Equal(t, map[string]any{"stream": "stdout"}, log.GetBody())
	} else {
		require.NoError(t, err)
		expected := map[string]any{"stream": "stdout"}
		maps.Copy(expected, want)
		require.Equal(t, expected, log.GetBody())
	}
	require.Zero(t, p.reader.Size())
}

func BenchmarkJsonParse(b *testing.B) {
	p, err := NewJsonParser(JsonParserConfig{})
	require.NoError(b, err)
	msg := `{"a": 1, "b": "2", "c": {"d": 3}}`
	log := types.NewLog()
	b.ReportAllocs()
	for b.Loop() {
		if err := p.Parse(log, msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJsonParseVersions(b *testing.B) {
	inputs := []struct {
		name string
		msg  string
	}{
		{"small", `{"a":1,"b":"2","c":{"d":3}}`},
		{"structured", `{"timestamp":"2026-11-05T00:00:00Z","level":"info","message":"request completed","http":{"method":"GET","path":"/api/v1/query","status":200,"duration_ms":12.5},"labels":{"namespace":"default","pod":"api-123","container":"server"},"retries":0,"success":true,"errors":null,"tags":["api","production"]}`},
		{"large", `{"level":"error","message":"` + strings.Repeat(`stack frame\n`, 1024) + `","context":{"attempt":3,"retry":true}}`},
	}
	for _, input := range inputs {
		b.Run(input.name, func(b *testing.B) {
			for _, version := range []string{"v1", "v2"} {
				b.Run(version, func(b *testing.B) {
					p, err := NewJsonParser(JsonParserConfig{})
					require.NoError(b, err)
					parse := p.Parse
					if version == "v1" {
						parse = func(log *types.Log, msg string) error {
							if len(msg) == 0 || msg[0] != '{' {
								return ErrNotJson
							}
							clear(p.parsed)
							if err := jsonv1.Unmarshal([]byte(msg), &p.parsed); err != nil {
								return ErrNotJson
							}
							for k, v := range p.parsed {
								log.SetBodyValue(k, v)
							}
							return nil
						}
					}
					log := types.NewLog()
					b.ReportAllocs()
					b.SetBytes(int64(len(input.msg)))
					for b.Loop() {
						if err := parse(log, input.msg); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
