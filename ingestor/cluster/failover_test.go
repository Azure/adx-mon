package cluster

import (
	"testing"
)

func TestFailoverState_Basic(t *testing.T) {
	f := NewFailoverState()
	if f.InFailover("db1") {
		t.Errorf("expected db1 not in failover")
	}
	f.SetFailover("db1", "instanceA")
	if !f.InFailover("db1") {
		t.Errorf("expected db1 in failover")
	}
	if got := f.GetFailover("db1"); got != "instanceA" {
		t.Errorf("expected instanceA, got %s", got)
	}
	f.ClearFailover("db1")
	if f.InFailover("db1") {
		t.Errorf("expected db1 not in failover after clear")
	}
}
