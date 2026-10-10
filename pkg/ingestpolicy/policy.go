// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

// Package ingestpolicy assigns an ingestion priority to destination tables.
//
// A Policy is immutable after construction and safe for concurrent use.  Lookups do not allocate so they can be
// used on the write and upload hot paths.
package ingestpolicy

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Azure/adx-mon/schema"
)

// Priority is the ingestion priority of a destination table.
type Priority uint8

const (
	// PriorityQueued is the default priority. Data is uploaded with queued ingestion.
	PriorityQueued Priority = iota
	// PriorityRealtime data is uploaded with streaming ingestion.
	PriorityRealtime
)

// NumPriorities is the number of defined priorities.  It can be used to size per-priority arrays.
const NumPriorities = int(PriorityRealtime) + 1

func (p Priority) String() string {
	switch p {
	case PriorityQueued:
		return "queued"
	case PriorityRealtime:
		return "realtime"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(p))
	}
}

// Table identifies a destination table.
type Table struct {
	Database string
	Table    string
}

func (t Table) String() string {
	return t.Database + "." + t.Table
}

// Policy maps destination tables to priorities.  The zero value and a nil *Policy assign PriorityQueued to every
// table.
type Policy struct {
	// realtime is keyed by normalized database then normalized table.
	realtime map[string]map[string]struct{}
	tables   []Table
}

// New returns a Policy where the given tables are realtime and all other tables are queued.  Database and table
// names are normalized the same way as WAL segment keys.  Names that are empty after normalization and duplicate
// tables are rejected.
func New(realtime []Table) (*Policy, error) {
	p := &Policy{}
	if len(realtime) == 0 {
		return p, nil
	}

	p.realtime = make(map[string]map[string]struct{})
	var errs []error
	for _, t := range realtime {
		db := schema.NormalizeAdxIdentifier(t.Database)
		table := schema.NormalizeAdxIdentifier(t.Table)
		if db == "" || table == "" {
			errs = append(errs, fmt.Errorf("invalid realtime table %q: database and table must contain at least one alphanumeric character", t.String()))
			continue
		}

		tables, ok := p.realtime[db]
		if !ok {
			tables = make(map[string]struct{})
			p.realtime[db] = tables
		}
		if _, ok := tables[table]; ok {
			errs = append(errs, fmt.Errorf("duplicate realtime table %q", db+"."+table))
			continue
		}
		tables[table] = struct{}{}
		p.tables = append(p.tables, Table{Database: db, Table: table})
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	sort.Slice(p.tables, func(i, j int) bool {
		if p.tables[i].Database != p.tables[j].Database {
			return p.tables[i].Database < p.tables[j].Database
		}
		return p.tables[i].Table < p.tables[j].Table
	})
	return p, nil
}

// Priority returns the priority for the normalized database and table.  For WAL segments, obtain the database and
// table with wal.ParseFilename.
func (p *Policy) Priority(database, table string) Priority {
	if p == nil || len(p.realtime) == 0 {
		return PriorityQueued
	}
	if _, ok := p.realtime[database][table]; ok {
		return PriorityRealtime
	}
	return PriorityQueued
}

// PriorityBytes is like Priority but accepts byte slices without allocating.
func (p *Policy) PriorityBytes(database, table []byte) Priority {
	if p == nil || len(p.realtime) == 0 {
		return PriorityQueued
	}
	if _, ok := p.realtime[string(database)][string(table)]; ok {
		return PriorityRealtime
	}
	return PriorityQueued
}

// HasRealtime returns true if any table is realtime.
func (p *Policy) HasRealtime() bool {
	return p != nil && len(p.realtime) > 0
}

// RealtimeTables returns the normalized realtime tables sorted by database then table.  The returned slice must not
// be modified.
func (p *Policy) RealtimeTables() []Table {
	if p == nil {
		return nil
	}
	return p.tables
}

// RealtimeDatabases returns the distinct normalized databases that contain realtime tables, sorted.
func (p *Policy) RealtimeDatabases() []string {
	if p == nil {
		return nil
	}
	dbs := make([]string, 0, len(p.realtime))
	for db := range p.realtime {
		dbs = append(dbs, db)
	}
	sort.Strings(dbs)
	return dbs
}
