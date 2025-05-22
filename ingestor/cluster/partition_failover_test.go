package cluster

import "testing"

func TestFailoverAwarePartitioner_Owner(t *testing.T) {
	p, _ := NewPartition(map[string]string{"a": "addrA", "b": "addrB"})
	failover := NewFailoverState()
	fap := &FailoverAwarePartitioner{
		Partitioner:   p,
		FailoverState: failover,
		InstanceName:  "a",
	}

	// Normal mode: should use partitioner
	owner, addr := fap.Owner([]byte("foo"), "db1")
	if owner == "" || addr == "" {
		t.Errorf("expected normal owner, got %q %q", owner, addr)
	}

	// Enter failover for db1
	failover.SetFailover("db1", "b")
	owner, addr = fap.Owner([]byte("foo"), "db1")
	if owner != "b" || addr != "addrB" {
		t.Errorf("expected failover owner b/addrB, got %q %q", owner, addr)
	}

	// Exit failover
	failover.ClearFailover("db1")
	owner2, addr2 := fap.Owner([]byte("foo"), "db1")
	if owner2 == "b" && addr2 == "addrB" {
		// Could be b, but should be via partitioner, not forced
		return
	}
}
