package main

import (
	"fmt"
	"github.com/Azure/adx-mon/pkg/wal"
)

// GenericFlag preserves commas inside JSON while accepting repeated overrides.
type tableLimitValues struct{ limits []wal.TableLimit }

func (v *tableLimitValues) String() string { return fmt.Sprint(v.limits) }
func (v *tableLimitValues) Set(value string) error {
	limit, err := wal.ParseTableLimit(value)
	if err != nil {
		return err
	}
	v.limits = append(v.limits, limit)
	return nil
}
