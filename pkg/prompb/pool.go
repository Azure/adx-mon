package prompb

import "sync"

type Resetter interface {
	Reset()
}

type Pool[T Resetter] struct {
	p   *sync.Pool
	New func() T
}

func NewPool[T Resetter](new func() T) *Pool[T] {
	return &Pool[T]{
		p:   &sync.Pool{New: func() interface{} { return new() }},
		New: new,
	}
}

func (p *Pool[T]) Put(r T) {
	r.Reset()
	p.p.Put(r)
}

func (p *Pool[T]) Get() T {
	return p.p.Get().(T)
}
