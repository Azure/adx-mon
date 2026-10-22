package operator

import "maps"

var operatorClusterLabels = map[string]string{}

// SetClusterLabels configures cluster labels that gate reconciliation for operator-managed resources.
// The provided map is cloned to avoid accidental mutation by callers.
func SetClusterLabels(labels map[string]string) {
	if labels == nil {
		operatorClusterLabels = map[string]string{}
		return
	}
	operatorClusterLabels = maps.Clone(labels)
}

func getOperatorClusterLabels() map[string]string {
	return maps.Clone(operatorClusterLabels)
}
