package cluster

import (
	"testing"
)

func TestBatcher_FailoverAwarePartitioner(t *testing.T) {
	p, _ := NewPartition(map[string]string{"a": "addrA", "b": "addrB"})
	failover := NewFailoverState()
	fap := &FailoverAwarePartitioner{
		Partitioner:   p,
		FailoverState: failover,
		InstanceName:  "a",
	}

	batcher := &batcher{
		Partitioner: fap,
		hostname:    "a",
	}

	// Normal mode: should use partitioner
	owner, _ := batcher.Partitioner.Owner([]byte("db1_Cpu"))
	if owner == "" {
		t.Errorf("expected normal owner, got empty")
	}

	// Enter failover for db1
	failover.SetFailover("db1", "b")
	owner, _ = fap.OwnerWithDB([]byte("db1_Cpu"), "db1")
	if owner != "b" {
		t.Errorf("expected failover owner b, got %q", owner)
	}
}
