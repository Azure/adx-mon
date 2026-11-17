package v1

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestAlerterSpecCRDSchemaAndCELValidation(t *testing.T) {
	paths := []string{
		"../../kustomize/bases/alerters_crd.yaml",
		"../../operator/manifests/crds/alerters_crd.yaml",
	}

	kustomizeBytes, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	operatorBytes, err := os.ReadFile(paths[1])
	require.NoError(t, err)
	require.Equal(t, string(kustomizeBytes), string(operatorBytes), "CRD manifests in kustomize/bases and operator/manifests/crds must match")

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)

			var crd apiextensionsv1.CustomResourceDefinition
			require.NoError(t, yaml.Unmarshal(data, &crd))
			require.NotEmpty(t, crd.Spec.Versions)

			for _, version := range crd.Spec.Versions {
				require.NotNil(t, version.Schema)
				require.NotNil(t, version.Schema.OpenAPIV3Schema)

				specSchema := version.Schema.OpenAPIV3Schema.Properties["spec"]
				require.Equal(t, []string{"adxClusterSelector"}, specSchema.Required)

				singleProp := specSchema.Properties["notificationEndpoint"]
				require.Equal(t, "string", singleProp.Type)
				require.Equal(t, "uri", singleProp.Format)
				require.NotNil(t, singleProp.MinLength)
				require.Equal(t, int64(1), *singleProp.MinLength)

				multiProp := specSchema.Properties["notificationEndpoints"]
				require.Equal(t, "array", multiProp.Type)
				require.NotNil(t, multiProp.MinItems)
				require.Equal(t, int64(1), *multiProp.MinItems)
				require.NotNil(t, multiProp.Items)
				require.NotNil(t, multiProp.Items.Schema)
				require.Equal(t, "string", multiProp.Items.Schema.Type)
				require.Equal(t, "uri", multiProp.Items.Schema.Format)
				require.NotNil(t, multiProp.Items.Schema.MinLength)
				require.Equal(t, int64(1), *multiProp.Items.Schema.MinLength)

				require.NotEmpty(t, specSchema.XValidations)
				rule := specSchema.XValidations[0].Rule
				require.Contains(t, rule, "has(self.notificationEndpoint)")
				require.Contains(t, rule, "has(self.notificationEndpoints)")

				env, err := cel.NewEnv(
					cel.Variable("self", cel.MapType(cel.StringType, cel.DynType)),
				)
				require.NoError(t, err)
				ast, issues := env.Compile(rule)
				require.NoError(t, issues.Err())
				prg, err := env.Program(ast)
				require.NoError(t, err)

				cases := []struct {
					name    string
					selfObj map[string]any
					valid   bool
				}{
					{
						name: "valid single notificationEndpoint only",
						selfObj: map[string]any{
							"notificationEndpoint": "http://notifier-a:8080",
						},
						valid: true,
					},
					{
						name: "valid multiple notificationEndpoints only",
						selfObj: map[string]any{
							"notificationEndpoints": []string{
								"http://notifier-a:8080",
								"https://notifier-b.example.com/hook",
							},
						},
						valid: true,
					},
					{
						name:    "invalid when neither field is present",
						selfObj: map[string]any{},
						valid:   false,
					},
					{
						name: "invalid when both fields are present",
						selfObj: map[string]any{
							"notificationEndpoint":  "http://notifier-a:8080",
							"notificationEndpoints": []string{"http://notifier-b:8080"},
						},
						valid: false,
					},
					{
						name: "invalid when notificationEndpoint is empty string",
						selfObj: map[string]any{
							"notificationEndpoint": "",
						},
						valid: false,
					},
					{
						name: "invalid when notificationEndpoints is empty list",
						selfObj: map[string]any{
							"notificationEndpoints": []string{},
						},
						valid: false,
					},
					{
						name: "invalid when notificationEndpoints contains empty item",
						selfObj: map[string]any{
							"notificationEndpoints": []string{"http://notifier-a:8080", ""},
						},
						valid: false,
					},
					{
						name: "invalid when single set alongside empty slice",
						selfObj: map[string]any{
							"notificationEndpoint":  "http://notifier-a:8080",
							"notificationEndpoints": []string{},
						},
						valid: false,
					},
				}

				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) {
						out, _, evalErr := prg.Eval(map[string]any{"self": tc.selfObj})
						require.NoError(t, evalErr)
						got, ok := out.Value().(bool)
						require.True(t, ok)
						require.Equal(t, tc.valid, got)
					})
				}
			}
		})
	}
}

func TestAlerterSpecDeepCopyAndJSON(t *testing.T) {
	orig := &AlerterSpec{
		Image: "ghcr.io/azure/adx-mon/alerter:v1",
		NotificationEndpoints: []string{
			"http://notifier-a:8080",
			"http://notifier-b:8080",
		},
		ADXClusterSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "adx-mon"},
		},
	}

	copied := orig.DeepCopy()
	copied.NotificationEndpoints[0] = "http://mutated:8080"
	require.Equal(t, "http://notifier-a:8080", orig.NotificationEndpoints[0])

	rawJSON, err := json.Marshal(orig)
	require.NoError(t, err)
	var asMap map[string]any
	require.NoError(t, json.Unmarshal(rawJSON, &asMap))
	require.NotContains(t, asMap, "notificationEndpoint")
	require.Contains(t, asMap, "notificationEndpoints")
}
