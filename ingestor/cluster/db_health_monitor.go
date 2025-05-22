package cluster

import (
	"sync"
)

// DatabaseHealthMonitor monitors the health of databases and manages failover state.
type DatabaseHealthMonitor struct {
	failover *FailoverState
	mu       sync.Mutex
	// unhealthy tracks which databases are currently unhealthy
	unhealthy map[string]bool
}

func NewDatabaseHealthMonitor(failover *FailoverState) *DatabaseHealthMonitor {
	return &DatabaseHealthMonitor{
		failover:  failover,
		unhealthy: make(map[string]bool),
	}
}

// SetUnhealthy marks a database as unhealthy and assigns a sacrificial instance.
func (m *DatabaseHealthMonitor) SetUnhealthy(db, instance string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.unhealthy[db] {
		m.unhealthy[db] = true
		m.failover.SetFailover(db, instance)
	}
}

// SetHealthy marks a database as healthy and clears failover state.
func (m *DatabaseHealthMonitor) SetHealthy(db string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unhealthy[db] {
		delete(m.unhealthy, db)
		m.failover.ClearFailover(db)
	}
}

// IsUnhealthy returns true if the database is currently unhealthy.
func (m *DatabaseHealthMonitor) IsUnhealthy(db string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.unhealthy[db]
}
