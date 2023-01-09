package transform

type labels map[string]string

func (l labels) Reset() {
	for k := range l {
		delete(l, k)
	}
}
