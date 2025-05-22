package cluster

import (
	"sync"
)

// FailoverState tracks which databases are in failover and which instance is the sacrificial owner for each.
type FailoverState struct {
	mu sync.RWMutex
	// dbName -> instanceName
	failover map[string]string
}

func NewFailoverState() *FailoverState {
	return &FailoverState{failover: make(map[string]string)}
}

// SetFailover assigns a database to a sacrificial instance.
func (f *FailoverState) SetFailover(db, instance string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failover[db] = instance
}

// ClearFailover removes a database from failover mode.
func (f *FailoverState) ClearFailover(db string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failover, db)
}

// GetFailover returns the sacrificial instance for a database, or empty string if not in failover.
func (f *FailoverState) GetFailover(db string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.failover[db]
}

// InFailover returns true if the database is in failover mode.
func (f *FailoverState) InFailover(db string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.failover[db]
	return ok
}
