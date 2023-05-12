package engine

import "fmt"

type UnknownDBError struct {
	DB string
}

func (e *UnknownDBError) Error() string {
	return fmt.Sprintf("no client for database %s", e.DB)
}
