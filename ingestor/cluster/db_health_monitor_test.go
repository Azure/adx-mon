package cluster

import "testing"

func TestDatabaseHealthMonitor_Basic(t *testing.T) {
	failover := NewFailoverState()
	monitor := NewDatabaseHealthMonitor(failover)

	if monitor.IsUnhealthy("db1") {
		t.Errorf("expected db1 healthy initially")
	}
	monitor.SetUnhealthy("db1", "instanceA")
	if !monitor.IsUnhealthy("db1") {
		t.Errorf("expected db1 unhealthy after SetUnhealthy")
	}
	if !failover.InFailover("db1") || failover.GetFailover("db1") != "instanceA" {
		t.Errorf("expected failover set for db1 to instanceA")
	}
	monitor.SetHealthy("db1")
	if monitor.IsUnhealthy("db1") {
		t.Errorf("expected db1 healthy after SetHealthy")
	}
	if failover.InFailover("db1") {
		t.Errorf("expected failover cleared for db1 after SetHealthy")
	}
}
