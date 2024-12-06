package adx

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto/data/errors"
	"github.com/Azure/azure-kusto-go/kusto/kql"
)

// KQLHandler implements cache.ResourceEventHandler and receives
// KQL CRD events and reconciles them with Kusto.
type KQLHandler struct {
	opts   KQLHandlerOpts
	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.RWMutex
	retryQueue []*retry
}

type UpdateStatusFunc func(ctx context.Context, fn *v1.Function, status v1.FunctionStatusEnum, err error)

type retry struct {
	fn       *v1.Function
	attempts int
	delete   bool
}

type KQLHandlerOpts struct {
	KustoCli      mgmt
	Database      string
	StatusFn      UpdateStatusFunc
	MaxRetry      int
	RetryInterval time.Duration
}

// TODO: Need leader election so that only 1 operator is mutating Kusto's state
// Can we make use of the way we determine if we're the owner of a particular table? See uploader.go

func NewKQLHandler(opts KQLHandlerOpts) *KQLHandler {
	if opts.StatusFn == nil {
		opts.StatusFn = noopUpdateStatusFn
	}
	if opts.RetryInterval == 0 {
		opts.RetryInterval = 10 * time.Minute
	}
	return &KQLHandler{
		opts: opts,
	}
}

func (h *KQLHandler) Open(ctx context.Context) error {
	h.ctx, h.cancel = context.WithCancel(ctx)

	go h.retries()
	return nil
}

func (h *KQLHandler) Close() error {
	h.cancel()
	return nil
}

func (h *KQLHandler) executeStmt(stmt *kql.Builder, fn *v1.Function) {
	status := v1.Success
	_, err := h.opts.KustoCli.Mgmt(h.ctx, h.opts.Database, stmt)
	if err != nil {

		if !errors.Retry(err) {
			status = v1.PermanentFailure
		} else {
			status = v1.Failed
		}

		// We want to retry even if Kusto thinks this is a permanent failure.
		// There are several reasons why Kusto might think it's a permanent failure,
		// like a Table not yet existing, where we will want to retry. However, we'll
		// make the CRD object as having the appropriate failure status so that
		// opertors can take appropriate action.
		h.mu.Lock()
		h.retryQueue = append(h.retryQueue, &retry{
			fn:     fn,
			delete: strings.HasPrefix(stmt.String(), ".drop"),
		})
		h.mu.Unlock()
	}

	h.opts.StatusFn(h.ctx, fn, status, err)
}

func (h *KQLHandler) OnAdd(obj interface{}, isInInitialList bool) {
	fn, ok := obj.(*v1.Function)
	if !ok {
		logger.Errorf("Invalid object type: %T", obj)
		return
	}
	stmt := kql.New("").AddUnsafe(fn.Spec.Body)
	h.executeStmt(stmt, fn)
}

func (h *KQLHandler) OnUpdate(oldObj, newObj interface{}) {
	oldFn := oldObj.(*v1.Function)
	newFn := newObj.(*v1.Function)
	if oldFn.GetGeneration() == newFn.GetGeneration() {
		return
	}
	h.OnAdd(newObj, false)
}

func (h *KQLHandler) OnDelete(obj interface{}) {
	fn, ok := obj.(*v1.Function)
	if !ok {
		logger.Errorf("Invalid object type: %T", obj)
		return
	}
	stmt := kql.New(".drop function ").AddUnsafe(fn.GetName()).AddLiteral(" ifexists")
	h.executeStmt(stmt, fn)
}

func (h *KQLHandler) retries() {
	ticker := time.NewTicker(h.opts.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.mu.Lock()
			var (
				retries    []*retry
				deadLetter []*retry
			)
			for _, retry := range h.retryQueue {
				retry.attempts++
				if retry.attempts < h.opts.MaxRetry {
					retries = append(retries, retry)
				} else {
					deadLetter = append(deadLetter, retry)
				}
			}
			h.mu.Unlock()

			for _, retry := range deadLetter {
				h.opts.StatusFn(h.ctx, retry.fn, v1.PermanentFailure, nil)
			}

			var deleteIndexes []int
			for idx, retry := range retries {
				var stmt *kql.Builder
				if retry.delete {
					stmt = kql.New(".drop function ").AddUnsafe(retry.fn.GetName()).AddLiteral(" ifexists")
				} else {
					stmt = kql.New("").AddUnsafe(retry.fn.Spec.Body)
				}
				if _, err := h.opts.KustoCli.Mgmt(h.ctx, h.opts.Database, stmt); err == nil {
					h.opts.StatusFn(h.ctx, retry.fn, v1.Success, nil)
					deleteIndexes = append(deleteIndexes, idx)
				}
			}

			h.mu.Lock()
			h.retryQueue = nil
			for idx, retry := range retries {
				if slices.Contains(deleteIndexes, idx) {
					continue
				}
				h.retryQueue = append(h.retryQueue, retry)
			}
			h.mu.Unlock()
		case <-h.ctx.Done():
			return
		}
	}
}

func noopUpdateStatusFn(ctx context.Context, fn *v1.Function, status v1.FunctionStatusEnum, err error) {
	switch status {
	case v1.Success:
		logger.Infof("Successfully %s.%s reconciled function", fn.Spec.Database, fn.Name)
	case v1.Failed:
		logger.Errorf("Failed to reconcile function %s.%s", fn.Spec.Database, fn.Name)
	case v1.PermanentFailure:
		logger.Errorf("PermanentFailure reconciling function %s.%s", fn.Spec.Database, fn.Name)
	}
}
