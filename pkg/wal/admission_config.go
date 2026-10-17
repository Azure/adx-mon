package wal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

var destinationIdentifier = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// ResolveAdmission applies per-instance defaults. Zero means 10% of the global
// capacity; disabling defaults still permits explicit table overrides.
func ResolveAdmission(global Usage, maxBytes, maxSegments int64, disabled bool, overrides []TableLimit) (AdmissionOpts, error) {
	return ResolveAdmissionWithEnforcement(global, maxBytes, maxSegments, disabled, false, overrides)
}

// ResolveAdmissionWithEnforcement resolves budgets and selects whether exceeding
// a table budget rejects writes or is only reported.
func ResolveAdmissionWithEnforcement(global Usage, maxBytes, maxSegments int64, disabled, enforce bool, overrides []TableLimit) (AdmissionOpts, error) {
	if maxBytes < 0 || maxSegments < 0 {
		return AdmissionOpts{}, fmt.Errorf("table budgets must not be negative")
	}
	if !disabled {
		if maxBytes == 0 && global.Bytes > 0 {
			maxBytes = max(global.Bytes/10, 8)
		}
		if maxSegments == 0 && global.Segments > 0 {
			maxSegments = max(global.Segments/10, 1)
		}
	} else {
		maxBytes = 0
		maxSegments = 0
	}
	seen := make(map[Destination]bool)
	for _, l := range overrides {
		d := Destination{l.Database, l.Table}
		if !destinationIdentifier.MatchString(l.Database) || !destinationIdentifier.MatchString(l.Table) {
			return AdmissionOpts{}, fmt.Errorf("table limit requires normalized database and table: %s", d)
		}
		if seen[d] {
			return AdmissionOpts{}, fmt.Errorf("duplicate table limit: %s", d)
		}
		seen[d] = true
		if l.MaxBytes < 0 || l.MaxSegments < 0 {
			return AdmissionOpts{}, fmt.Errorf("negative table limit: %s", d)
		}
		if l.MaxBytes > 0 && l.MaxBytes < 8 {
			return AdmissionOpts{}, fmt.Errorf("table byte limit must fit a WAL header: %s", d)
		}
		if global.Bytes > 0 && l.MaxBytes > global.Bytes || global.Segments > 0 && l.MaxSegments > global.Segments {
			return AdmissionOpts{}, fmt.Errorf("table limit exceeds global budget: %s", d)
		}
	}
	if maxBytes > 0 && maxBytes < 8 || global.Bytes > 0 && maxBytes > global.Bytes || global.Segments > 0 && maxSegments > global.Segments {
		return AdmissionOpts{}, fmt.Errorf("invalid default table budget")
	}
	return AdmissionOpts{Global: global, Default: Usage{maxBytes, maxSegments}, Overrides: overrides, Enforce: enforce}, nil
}

func ParseTableLimit(value string) (TableLimit, error) {
	var limit TableLimit
	d := json.NewDecoder(bytes.NewBufferString(value))
	d.DisallowUnknownFields()
	if err := d.Decode(&limit); err != nil {
		return limit, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return limit, fmt.Errorf("table limit must be one JSON object")
	}
	return limit, nil
}
