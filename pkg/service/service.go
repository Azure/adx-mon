package service

import "context"

type Component interface {
	Open(ctx context.Context) error
	Close() error
}
