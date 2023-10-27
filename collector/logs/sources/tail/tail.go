package tail

import "github.com/Azure/adx-mon/collector/logs/types"

type TailSource struct {
	outputQueue chan *types.LogBatch
}

func NewTailSource() *TailSource {
	return &TailSource{}
}

func (s *TailSource) Open() error {
	return nil
}

func (s *TailSource) Close() error {
	return nil
}

func (s *TailSource) Name() string {
	return "tailsource"
}

func (s *TailSource) Queue() <-chan *types.LogBatch {
	return s.outputQueue
}
