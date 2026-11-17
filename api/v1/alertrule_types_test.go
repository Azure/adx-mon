package v1_test

import (
	"encoding/json"
	"os"
	"testing"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

type schemaBoundsCostEstimator struct {
	maxItems      uint64
	maxItemLength uint64
}

func (e schemaBoundsCostEstimator) EstimateSize(element checker.AstNode) *checker.SizeEstimate {
	p := element.Path()
	if len(p) == 1 && p[0] == "self" {
		return &checker.SizeEstimate{Min: 0, Max: e.maxItems}
	}
	if len(p) == 2 && p[0] == "self" && p[1] == "@items" {
		return &checker.SizeEstimate{Min: 0, Max: e.maxItemLength}
	}
	return nil
}

func (e schemaBoundsCostEstimator) EstimateCallCost(string, string, *checker.AstNode, []checker.AstNode) *checker.CallEstimate {
	return nil
}

func TestAlertRuleSpec_UnmarshalJSON_CriteriaList(t *testing.T) {
	data := `{"autoMitigateAfter":"1h","criteria":{"cloud":["AzureCloud","AGC"]},"database":"DB","destination":"Destination","interval":"5m","query":"Query\n"}`
	a := &v1.AlertRuleSpec{}
	err := a.UnmarshalJSON([]byte(data))
	require.NoError(t, err)
	require.Equal(t, "1h0m0s", a.AutoMitigateAfter.Duration.String())
	require.Equal(t, "DB", a.Database)
	require.Equal(t, "Destination", a.Destination)
	require.Nil(t, a.Destinations)
	require.Equal(t, "5m0s", a.Interval.Duration.String())
	require.Equal(t, "Query\n", a.Query)
	require.Equal(t, map[string][]string{"cloud": {"AzureCloud", "AGC"}}, a.Criteria)
}

func TestAlertRuleSpec_UnmarshalJSON_CriteriaString(t *testing.T) {
	// This is an example of the old format for the Criteria field.  It is a map[string]string instead of a map[string][]string.
	data := `{"autoMitigateAfter":"1h","criteria":{"cloud":"AzureCloud"},"database":"DB","destination":"Destination","interval":"5m","query":"Query\n"}`
	a := &v1.AlertRuleSpec{}
	err := a.UnmarshalJSON([]byte(data))
	require.NoError(t, err)
	require.Equal(t, "1h0m0s", a.AutoMitigateAfter.Duration.String())
	require.Equal(t, "DB", a.Database)
	require.Equal(t, "Destination", a.Destination)
	require.Equal(t, "5m0s", a.Interval.Duration.String())
	require.Equal(t, "Query\n", a.Query)
	require.Equal(t, map[string][]string{"cloud": {"AzureCloud"}}, a.Criteria)
}

func TestAlertRuleSpec_UnmarshalJSON_DestinationsAndOptionalFields(t *testing.T) {
	t.Run("destinations without destination preserves order and mixed criteria", func(t *testing.T) {
		data := `{
			"database": "Metrics",
			"interval": "5m",
			"query": "Cpu | take 1",
			"autoMitigateAfter": "",
			"destinations": ["pagerduty://team-b", "icm://team-a", "webhook://ops"],
			"criteria": {"cloud": "public", "region": ["eastus", "westus"]},
			"criteriaExpression": "env == 'prod'"
		}`
		a := &v1.AlertRuleSpec{Destination: "stale"}
		require.NoError(t, json.Unmarshal([]byte(data), a))
		require.Equal(t, "Metrics", a.Database)
		require.Equal(t, "", a.Destination)
		require.Equal(t, []string{"pagerduty://team-b", "icm://team-a", "webhook://ops"}, a.Destinations)
		require.Zero(t, a.AutoMitigateAfter.Duration)
		require.Equal(t, map[string][]string{
			"cloud":  {"public"},
			"region": {"eastus", "westus"},
		}, a.Criteria)
		require.Equal(t, "env == 'prod'", a.CriteriaExpression)

		copied := a.DeepCopy()
		copied.Destinations[0] = "mutated"
		require.Equal(t, "pagerduty://team-b", a.Destinations[0])
	})

	t.Run("empty object and null fields do not panic", func(t *testing.T) {
		a := &v1.AlertRuleSpec{}
		require.NoError(t, json.Unmarshal([]byte(`{}`), a))
		require.Equal(t, &v1.AlertRuleSpec{}, a)

		require.NoError(t, json.Unmarshal([]byte(`{"database":null,"destination":null,"destinations":null,"interval":null,"query":null,"autoMitigateAfter":null}`), a))
		require.Equal(t, &v1.AlertRuleSpec{}, a)

		a.Database = "nonempty"
		require.NoError(t, a.UnmarshalJSON([]byte(`null`)))
		require.Equal(t, &v1.AlertRuleSpec{}, a)
	})

	t.Run("wrong field types return descriptive errors without panicking", func(t *testing.T) {
		badPayloads := []string{
			`{"database": 123}`,
			`{"interval": 30}`,
			`{"interval": "not-a-duration"}`,
			`{"query": ["bad"]}`,
			`{"autoMitigateAfter": true}`,
			`{"autoMitigateAfter": "bad-duration"}`,
			`{"destination": 42}`,
			`{"destinations": "not-an-array"}`,
			`{"destinations": ["ok", 99]}`,
			`{"criteria": "not-an-object"}`,
			`{"criteria": {"cloud": 123}}`,
			`{"criteria": {"cloud": ["ok", 123]}}`,
			`{"criteriaExpression": 123}`,
		}
		for _, payload := range badPayloads {
			var spec v1.AlertRuleSpec
			require.Error(t, json.Unmarshal([]byte(payload), &spec), "payload=%s", payload)
		}
	})
}

func TestAlertRuleCRDSchemaAndCELValidation(t *testing.T) {
	paths := []string{
		"../../kustomize/bases/alertrules_crd.yaml",
		"../../operator/manifests/crds/alertrules_crd.yaml",
	}

	kustomizeBytes, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	operatorBytes, err := os.ReadFile(paths[1])
	require.NoError(t, err)
	require.Equal(t, string(kustomizeBytes), string(operatorBytes), "AlertRule CRD manifests must stay identical")

	var crd apiextensionsv1.CustomResourceDefinition
	require.NoError(t, yaml.Unmarshal(kustomizeBytes, &crd))
	require.NotEmpty(t, crd.Spec.Versions)

	for _, version := range crd.Spec.Versions {
		specSchema := version.Schema.OpenAPIV3Schema.Properties["spec"]
		destsProp := specSchema.Properties["destinations"]
		require.Equal(t, "array", destsProp.Type)
		require.Nil(t, destsProp.XListType, "destinations must preserve authored order (not set)")
		require.NotNil(t, destsProp.MaxItems)
		require.Equal(t, int64(v1.MaxAlertRuleDestinations), *destsProp.MaxItems)
		require.NotNil(t, destsProp.Items)
		require.NotNil(t, destsProp.Items.Schema)
		require.Equal(t, "string", destsProp.Items.Schema.Type)
		require.NotNil(t, destsProp.Items.Schema.MinLength)
		require.Equal(t, int64(1), *destsProp.Items.Schema.MinLength)
		require.NotNil(t, destsProp.Items.Schema.MaxLength, "items.maxLength is required to bound Kubernetes CEL admission cost")
		require.Equal(t, int64(v1.MaxAlertRuleDestinationLength), *destsProp.Items.Schema.MaxLength)

		sourceBytes, err := os.ReadFile("alertrule_types.go")
		require.NoError(t, err)
		for _, line := range bytesSplitLines(sourceBytes) {
			if containsKubebuilderMarker(line) {
				for _, b := range line {
					require.True(t, b >= 0x20 && b <= 0x7e || b == '\t', "non-ASCII byte %#x in kubebuilder marker: %s", b, string(line))
				}
			}
		}

		statusSchema := version.Schema.OpenAPIV3Schema.Properties["status"]
		condsProp := statusSchema.Properties["conditions"]
		require.NotNil(t, condsProp.XListType)
		require.Equal(t, "map", *condsProp.XListType)
		require.Equal(t, []string{"type"}, condsProp.XListMapKeys)

		// 1. Evaluate spec-level destination XOR destinations mutual exclusion CEL rule.
		require.NotEmpty(t, specSchema.XValidations)
		specEnv, err := cel.NewEnv(cel.Variable("self", cel.MapType(cel.StringType, cel.DynType)))
		require.NoError(t, err)
		for _, xv := range specSchema.XValidations {
			ast, issues := specEnv.Compile(xv.Rule)
			require.NoError(t, issues.Err())
			prg, err := specEnv.Program(ast)
			require.NoError(t, err)

			for _, tc := range []struct {
				name  string
				self  map[string]any
				valid bool
			}{
				{name: "neither set", self: map[string]any{}, valid: true},
				{name: "destination only", self: map[string]any{"destination": "icm://team"}, valid: true},
				{name: "destinations only", self: map[string]any{"destinations": []string{"icm://team", "pagerduty://ops"}}, valid: true},
				{name: "empty destination with destinations", self: map[string]any{"destination": "", "destinations": []string{"icm://team"}}, valid: true},
				{name: "destination with empty destinations", self: map[string]any{"destination": "icm://team", "destinations": []string{}}, valid: true},
				{name: "both non-empty invalid", self: map[string]any{"destination": "icm://team", "destinations": []string{"pagerduty://ops"}}, valid: false},
			} {
				out, _, err := prg.Eval(map[string]any{"self": tc.self})
				require.NoError(t, err, tc.name)
				require.Equal(t, tc.valid, out.Value(), tc.name)
			}
		}

		// 2. Evaluate spec.destinations uniqueness CEL rule and verify Kubernetes static admission cost budget.
		const k8sPerRuleStaticCostLimit = 10_000_000
		const k8sUnboundedFallbackBytes = 3_145_728
		maxItemsBound := uint64(k8sUnboundedFallbackBytes)
		if destsProp.MaxItems != nil {
			maxItemsBound = uint64(*destsProp.MaxItems)
		}
		maxItemLenBound := uint64(k8sUnboundedFallbackBytes)
		if destsProp.Items != nil && destsProp.Items.Schema != nil && destsProp.Items.Schema.MaxLength != nil {
			// Kubernetes conservatively multiplies OpenAPI string maxLength (code points) by 4 UTF-8 bytes.
			maxItemLenBound = uint64(*destsProp.Items.Schema.MaxLength) * 4
		}

		require.NotEmpty(t, destsProp.XValidations)
		listEnv, err := cel.NewEnv(cel.Variable("self", cel.ListType(cel.StringType)))
		require.NoError(t, err)
		for _, xv := range destsProp.XValidations {
			ast, issues := listEnv.Compile(xv.Rule)
			require.NoError(t, issues.Err())

			costEst, err := listEnv.EstimateCost(ast, schemaBoundsCostEstimator{
				maxItems:      maxItemsBound,
				maxItemLength: maxItemLenBound,
			})
			require.NoError(t, err)
			require.Less(t, costEst.Max, uint64(k8sPerRuleStaticCostLimit),
				"CEL uniqueness rule static cost (%d) must stay well under Kubernetes 10M admission limit", costEst.Max)

			prg, err := listEnv.Program(ast)
			require.NoError(t, err)

			for _, tc := range []struct {
				name  string
				self  []string
				valid bool
			}{
				{name: "empty slice", self: []string{}, valid: true},
				{name: "single item", self: []string{"icm://a"}, valid: true},
				{name: "unique ordered items", self: []string{"pagerduty://b", "icm://a", "datadog://c"}, valid: true},
				{name: "adjacent duplicates", self: []string{"icm://a", "icm://a"}, valid: false},
				{name: "non-adjacent duplicates", self: []string{"icm://a", "pagerduty://b", "icm://a"}, valid: false},
			} {
				out, _, err := prg.Eval(map[string]any{"self": tc.self})
				require.NoError(t, err, tc.name)
				require.Equal(t, tc.valid, out.Value(), tc.name)
			}
		}

		// 3. Evaluate spec.interval and spec.autoMitigateAfter CEL rules.
		strEnv, err := cel.NewEnv(cel.Variable("self", cel.StringType))
		require.NoError(t, err)
		intervalProp := specSchema.Properties["interval"]
		require.NotEmpty(t, intervalProp.XValidations)
		for _, xv := range intervalProp.XValidations {
			ast, issues := strEnv.Compile(xv.Rule)
			require.NoError(t, issues.Err())
			prg, err := strEnv.Program(ast)
			require.NoError(t, err)

			outValid, _, err := prg.Eval(map[string]any{"self": "30s"})
			require.NoError(t, err)
			require.Equal(t, true, outValid.Value())

			outInvalid, _, err := prg.Eval(map[string]any{"self": "10s"})
			require.NoError(t, err)
			require.Equal(t, false, outInvalid.Value())
		}

		mitProp := specSchema.Properties["autoMitigateAfter"]
		require.NotEmpty(t, mitProp.XValidations)
		for _, xv := range mitProp.XValidations {
			ast, issues := strEnv.Compile(xv.Rule)
			require.NoError(t, issues.Err())
			prg, err := strEnv.Program(ast)
			require.NoError(t, err)

			outEmpty, _, err := prg.Eval(map[string]any{"self": ""})
			require.NoError(t, err)
			require.Equal(t, true, outEmpty.Value())

			outDur, _, err := prg.Eval(map[string]any{"self": "1h"})
			require.NoError(t, err)
			require.Equal(t, true, outDur.Value())
		}
	}
}

func bytesSplitLines(b []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			lines = append(lines, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, b[start:])
	}
	return lines
}

func containsKubebuilderMarker(line []byte) bool {
	const needle = "+kubebuilder:validation:"
	for i := 0; i+len(needle) <= len(line); i++ {
		if string(line[i:i+len(needle)]) == needle {
			return true
		}
	}
	return false
}
