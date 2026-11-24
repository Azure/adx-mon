package alert_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/stretchr/testify/require"
)

func TestCompileMatcher_SupportedKeywordsAndCombinators(t *testing.T) {
	t.Run("boolean true and false schemas", func(t *testing.T) {
		mTrue, err := alert.CompileMatcher([]byte(`true`))
		require.NoError(t, err)
		require.True(t, mTrue.Match(""))
		require.True(t, mTrue.Match("icm://oncall"))

		mFalse, err := alert.CompileMatcher([]byte(`false`))
		require.NoError(t, err)
		require.False(t, mFalse.Match(""))
		require.False(t, mFalse.Match("icm://oncall"))
	})

	t.Run("type string with metadata annotations and anyOf const pattern enum", func(t *testing.T) {
		schema := `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://receiver.example/destinations.schema.json",
			"title": "Accepted destinations",
			"description": "PagerDuty, IcM, and Datadog targets",
			"$comment": "allow-listed subset",
			"examples": ["pagerduty", "icm://compute"],
			"type": "string",
			"anyOf": [
				{ "const": "pagerduty" },
				{ "pattern": "^(icm|datadog)://" },
				{ "enum": ["slack://infra", "slack://sre"] }
			]
		}`
		m, err := alert.CompileMatcher([]byte(schema))
		require.NoError(t, err)

		require.True(t, m.Match("pagerduty"))
		require.True(t, m.Match("icm://compute"))
		require.True(t, m.Match("datadog://alerts"))
		require.True(t, m.Match("slack://sre"))
		require.False(t, m.Match("slack://general"))
		require.False(t, m.Match("email://ops"))
		require.False(t, m.Match(""))

		filtered := m.Filter([]string{"email://ops", "datadog://alerts", "pagerduty", "slack://general", "slack://infra"})
		require.Equal(t, []string{"datadog://alerts", "pagerduty", "slack://infra"}, filtered)
	})

	t.Run("exact-1 oneOf rejects zero or multiple matches", func(t *testing.T) {
		schema := `{
			"type": "string",
			"oneOf": [
				{ "pattern": "^icm://" },
				{ "pattern": "critical$" }
			]
		}`
		m, err := alert.CompileMatcher([]byte(schema))
		require.NoError(t, err)

		require.True(t, m.Match("icm://standard"), "matches first branch only")
		require.True(t, m.Match("pagerduty://critical"), "matches second branch only")
		require.False(t, m.Match("icm://critical"), "matches both branches -> rejected by exact-1 oneOf")
		require.False(t, m.Match("pagerduty://standard"), "matches zero branches")
	})

	t.Run("allOf and sibling keyword AND composition", func(t *testing.T) {
		schema := `{
			"type": "string",
			"pattern": "^icm://",
			"enum": ["icm://team-a", "icm://team-b", "pagerduty://team-a"],
			"allOf": [
				{ "pattern": "team-a$" }
			]
		}`
		m, err := alert.CompileMatcher([]byte(schema))
		require.NoError(t, err)

		require.True(t, m.Match("icm://team-a"))
		require.False(t, m.Match("icm://team-b"), "rejected by allOf")
		require.False(t, m.Match("pagerduty://team-a"), "rejected by sibling pattern")
	})
}

func TestCompileMatcher_BoundsAndFailClosedRejection(t *testing.T) {
	t.Run("depth <= 8 succeeds and depth > 8 fails closed", func(t *testing.T) {
		buildNested := func(depth int) string {
			s := `{"const":"target"}`
			for range depth {
				s = fmt.Sprintf(`{"anyOf":[%s]}`, s)
			}
			return s
		}

		m8, err := alert.CompileMatcher([]byte(buildNested(alert.MaxSchemaDepth)))
		require.NoError(t, err)
		require.True(t, m8.Match("target"))
		require.False(t, m8.Match("other"))

		_, err = alert.CompileMatcher([]byte(buildNested(alert.MaxSchemaDepth + 1)))
		require.ErrorContains(t, err, "maximum nesting depth")
	})

	t.Run("patterns <= 64 succeeds and > 64 fails closed", func(t *testing.T) {
		buildPatterns := func(n int) string {
			parts := make([]string, n)
			for i := range n {
				parts[i] = fmt.Sprintf(`{"pattern":"^dest-%d$"}`, i)
			}
			return fmt.Sprintf(`{"anyOf":[%s]}`, strings.Join(parts, ","))
		}

		m64, err := alert.CompileMatcher([]byte(buildPatterns(alert.MaxSchemaPatterns)))
		require.NoError(t, err)
		require.True(t, m64.Match("dest-63"))
		require.False(t, m64.Match("dest-64"))

		_, err = alert.CompileMatcher([]byte(buildPatterns(alert.MaxSchemaPatterns + 1)))
		require.ErrorContains(t, err, "maximum pattern count")
	})

	t.Run("unknown keywords, invalid types, invalid regex, and malformed JSON fail closed", func(t *testing.T) {
		rejected := []string{
			``,
			`null`,
			`123`,
			`"string"`,
			`[]`,
			`true false`,
			`{"const":"a","const":"b"}`,
			`{"not":{"const":"a"}}`,
			`{"$ref":"#/$defs/dest"}`,
			`{"properties":{"x":{"type":"string"}}}`,
			`{"items":{"type":"string"}}`,
			`{"minLength":1}`,
			`{"type":"number"}`,
			`{"type":["string","null"]}`,
			`{"const":123}`,
			`{"const":null}`,
			`{"enum":[]}`,
			`{"enum":["ok", 123]}`,
			`{"enum":["ok", null]}`,
			`{"pattern":"[unclosed"}`,
			`{"anyOf":[]}`,
			`{"oneOf":[]}`,
			`{"allOf":[]}`,
			`{"title":123}`,
			`{"examples":"not-an-array"}`,
		}
		for _, bad := range rejected {
			m, err := alert.CompileMatcher([]byte(bad))
			require.Error(t, err, "expected error for schema: %s", bad)
			require.Nil(t, m)
		}
	})
}
