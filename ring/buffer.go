package ring

type Entry struct {
	Value []byte
	ErrCh chan error
}
type Buffer struct {
	available, queued chan *Entry
}

func NewBuffer(size int) *Buffer {
	b := &Buffer{
		make(chan *Entry, size),
		make(chan *Entry, size),
	}

	for i := 0; i < size; i++ {
		b.available <- &Entry{ErrCh: make(chan error, 1)}
	}
	return b
}

func (b *Buffer) Reserve() *Entry {
	return <-b.available
}

func (b *Buffer) Enqueue(entry *Entry) {
	b.queued <- entry
}

func (b *Buffer) Queue() chan *Entry {
	return b.queued
}

func (b *Buffer) Release(entry *Entry) {
	b.available <- entry
}
