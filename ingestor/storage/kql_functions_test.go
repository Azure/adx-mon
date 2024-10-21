package storage

import (
	"context"
	"testing"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestViews(t *testing.T) {
	tests := []struct {
		Name            string
		In              client.ObjectList
		ViewsByName     map[string][]string
		FunctionsByName map[string][]string
	}{
		{
			Name: "Happy path",
			In: &v1.FunctionList{
				TypeMeta: metav1.TypeMeta{
					Kind: "FunctionList",
				},
				Items: []v1.Function{
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						Spec: v1.FunctionSpec{
							IsView:   true,
							Database: "A",
							Table:    "B",
							Name:     "B",
						},
					},
				},
			},
			ViewsByName: map[string][]string{
				"A": {"B"},
			},
		},
		{
			Name: "Functions mixed with Views",
			In: &v1.FunctionList{
				TypeMeta: metav1.TypeMeta{
					Kind: "FunctionList",
				},
				Items: []v1.Function{
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						Spec: v1.FunctionSpec{
							IsView:   true,
							Database: "A",
							Table:    "B",
							Name:     "B",
						},
					},
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						Spec: v1.FunctionSpec{
							Database: "A",
							Table:    "B",
							Name:     "C",
						},
					},
				},
			},
			ViewsByName: map[string][]string{
				"A": {"B"},
			},
			FunctionsByName: map[string][]string{
				"A": {"C"},
			},
		},
		{
			Name: "Invalid view configuration",
			In: &v1.FunctionList{
				TypeMeta: metav1.TypeMeta{
					Kind: "FunctionList",
				},
				Items: []v1.Function{
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						Spec: v1.FunctionSpec{
							IsView:   true,
							Database: "A",
							Table:    "B",
							Name:     "B",
						},
					},
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						Spec: v1.FunctionSpec{
							IsView:   true,
							Database: "A",
							Table:    "C",
							Name:     "D",
						},
					},
				},
			},
			ViewsByName: map[string][]string{
				"A": {"B"},
			},
		},
		{
			Name: "Duplicate functions",
			In: &v1.FunctionList{
				TypeMeta: metav1.TypeMeta{
					Kind: "FunctionList",
				},
				Items: []v1.Function{
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "A",
						},
						Spec: v1.FunctionSpec{
							IsView:   true,
							Database: "A",
							Table:    "B",
							Name:     "B",
						},
					},
					{
						TypeMeta: metav1.TypeMeta{
							Kind: "Function",
						},
						ObjectMeta: metav1.ObjectMeta{
							Name: "B",
						},
						Spec: v1.FunctionSpec{
							IsView:   true,
							Database: "A",
							Table:    "B",
							Name:     "B",
						},
					},
				},
			},
			ViewsByName: map[string][]string{
				"A": {"B"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			fns := &Functions{}
			require.NoError(t, fns.Receive(context.Background(), tt.In))

			for db, tables := range tt.ViewsByName {
				for _, table := range tables {
					v, ok := fns.View(db, table)
					require.True(t, ok)
					require.Equal(t, table, v.Spec.Name)
					require.Equal(t, table, v.Spec.Table)
					require.Equal(t, db, v.Spec.Database)
					require.True(t, v.Spec.IsView)
				}
			}

			var expectViews int
			for _, v := range tt.ViewsByName {
				expectViews += len(v)
			}
			var expectFunctions int
			for _, f := range tt.FunctionsByName {
				expectFunctions += len(f)
			}

			require.Equal(t, expectViews, len(fns.views.Items))
			require.Equal(t, expectFunctions, len(fns.functions.Items))
		})
	}
}
