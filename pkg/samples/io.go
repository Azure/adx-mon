package samples

import "context"

type Writer interface {
	Write(ctx context.Context, samples interface{}) error
}
