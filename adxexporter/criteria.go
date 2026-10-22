package adxexporter

import (
	"fmt"
	"strings"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/celutil"
)

// EvaluateExecutionCriteria evaluates the criteria map (OR semantics within values, any key match) and optional
// criteriaExpression (CEL). Semantics:
//   - Neither provided: proceed=true
//   - Only map: proceed when map matches
//   - Only expression: proceed when expression evaluates true
//   - Both provided: AND semantics (map must match AND expression true)
//
// Returns (proceed, reason, message, err) where err is only non-nil for expression parse/type/eval errors.
func EvaluateExecutionCriteria(criteria map[string][]string, criteriaExpression string, clusterLabels map[string]string) (bool, string, string, error) {
	mapProvided := len(criteria) > 0
	exprProvided := strings.TrimSpace(criteriaExpression) != ""

	mapMatch := matchesCriteria(criteria, clusterLabels)

	// Neither provided: allow
	if !mapProvided && !exprProvided {
		return true, adxmonv1.ReasonCriteriaMatched, "no criteria specified (default allow)", nil
	}

	// Evaluate expression if provided
	exprMatch := true
	if exprProvided {
		ok, err := celutil.EvaluateCriteriaExpression(clusterLabels, criteriaExpression)
		if err != nil {
			return false, adxmonv1.ReasonCriteriaExpressionError, fmt.Sprintf("criteria expression error: %v", err), err
		}
		exprMatch = ok
	}

	switch {
	case mapProvided && exprProvided:
		if mapMatch && exprMatch {
			return true, adxmonv1.ReasonCriteriaMatched, "criteria map matched and expression true", nil
		}
		if !mapMatch && exprMatch { // expression true, map failed
			return false, adxmonv1.ReasonCriteriaNotMatched, "criteria map did not match", nil
		}
		if mapMatch && !exprMatch { // map matched, expression false
			return false, adxmonv1.ReasonCriteriaNotMatched, "criteria expression evaluated to false", nil
		}
		return false, adxmonv1.ReasonCriteriaNotMatched, "criteria map did not match AND expression false", nil
	case mapProvided: // only map
		if mapMatch {
			return true, adxmonv1.ReasonCriteriaMatched, "criteria map matched", nil
		}
		return false, adxmonv1.ReasonCriteriaNotMatched, "criteria map did not match", nil
	case exprProvided: // only expression
		if exprMatch {
			return true, adxmonv1.ReasonCriteriaMatched, "criteria expression true", nil
		}
		return false, adxmonv1.ReasonCriteriaNotMatched, "criteria expression evaluated to false", nil
	}
	// Fallback: criteria not matched
	return false, adxmonv1.ReasonCriteriaNotMatched, "criteria not matched", nil
}

// matchesCriteria reused from existing logic (empty => true, case-insensitive key search; any value match returns true)
func matchesCriteria(criteria map[string][]string, clusterLabels map[string]string) bool {
	if len(criteria) == 0 {
		return true
	}
	for k, values := range criteria {
		lk := strings.ToLower(k)
		for ck, cv := range clusterLabels {
			if strings.ToLower(ck) == lk {
				for _, want := range values {
					if strings.EqualFold(cv, want) {
						return true
					}
				}
				break
			}
		}
	}
	return false
}
