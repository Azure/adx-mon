package celutil

import (
    "fmt"
    "strings"

    "github.com/google/cel-go/cel"
)

// EvaluateCriteriaExpression builds a CEL environment from the provided cluster labels (keys & values lowercased)
// and evaluates the given expression. It returns (true, nil) if the expression evaluates to the boolean true.
// If the expression evaluates to boolean false it returns (false, nil). Any error in environment construction,
// parsing, type checking, program build, evaluation, or non-boolean result returns a wrapped error with a
// generic message suitable for higher-level contextual wrapping by callers.
func EvaluateCriteriaExpression(clusterLabels map[string]string, expression string) (bool, error) {
    if expression == "" { // empty means implicit true for caller semantics
        return true, nil
    }
    // Normalize and prepare declarations
    decls := make([]cel.EnvOption, 0, len(clusterLabels))
    activation := make(map[string]interface{}, len(clusterLabels))
    for k, v := range clusterLabels {
        lk := strings.ToLower(k)
        decls = append(decls, cel.Variable(lk, cel.StringType))
        activation[lk] = strings.ToLower(v)
    }
    env, err := cel.NewEnv(decls...)
    if err != nil {
        return false, fmt.Errorf("cel env: %w", err)
    }
    ast, iss := env.Parse(expression)
    if iss.Err() != nil {
        return false, fmt.Errorf("cel parse: %w", iss.Err())
    }
    checked, iss2 := env.Check(ast)
    if iss2.Err() != nil {
        return false, fmt.Errorf("cel typecheck: %w", iss2.Err())
    }
    prog, err := env.Program(checked)
    if err != nil {
        return false, fmt.Errorf("cel program: %w", err)
    }
    out, _, err := prog.Eval(activation)
    if err != nil {
        return false, fmt.Errorf("cel eval: %w", err)
    }
    b, ok := out.Value().(bool)
    if !ok {
        return false, fmt.Errorf("cel result: non-bool value")
    }
    return b, nil
}
