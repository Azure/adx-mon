package logs

import "testing"

func TestCopy(t *testing.T) {
	log := &Log{
		Timestamp:         1,
		ObservedTimestamp: 2,
		Body: map[string]any{
			"key": "value",
			"complicated": map[string]any{
				"hello": "world",
			},
		},
		Attributes: map[string]any{
			"destination": "mdsd.ifxaudit",
			"k8s.pod.labels": map[string]string{
				"app": "myapp",
			},
		},
	}

	copy := log.Copy()
	copy.Attributes["destination"] = "mdsd.apiqos"

	if log.Attributes["destination"] != "mdsd.ifxaudit" {
		t.Errorf("expected destination to be mdsd.ifxaudit, got %v", log.Attributes["destination"])
	}
	if copy.Attributes["destination"] != "mdsd.apiqos" {
		t.Errorf("expected destination to be mdsd.apiqos, got %v", copy.Attributes["destination"])
	}
}
