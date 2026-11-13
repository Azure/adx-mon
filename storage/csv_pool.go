package storage

import "github.com/Azure/adx-mon/pkg/pool"

// maxRetainedCSVCapacity bounds total payload capacity per idle writer, not each
// individual buffer. The 8 KiB checkout and ordinary batches (e.g. the native-log
// benchmark's 250 records/table) fit comfortably; multi-MiB bursts are discarded.
// This bounds high-water retention in the persistent channel-backed pools.
const maxRetainedCSVCapacity = 256 << 10

type pooledCSVWriter interface {
	Reset()
	RetainedCapacity() int
}

type csvPool struct{ *pool.Generic }

func newCSVPool(newWriter func(int) any) *csvPool {
	return &csvPool{pool.NewGeneric(1000, newWriter)}
}

func (p *csvPool) Put(value any) {
	w := value.(pooledCSVWriter)
	if w.RetainedCapacity() > maxRetainedCSVCapacity {
		return
	}
	w.Reset()
	p.Generic.Put(value)
}
