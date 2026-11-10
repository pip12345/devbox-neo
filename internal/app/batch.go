package app

import "context"

type BatchStatus string

const (
	BatchSucceeded BatchStatus = "succeeded"
	BatchUnchanged BatchStatus = "unchanged"
	BatchSkipped   BatchStatus = "skipped"
	BatchFailed    BatchStatus = "failed"
)

type BatchResult[T any] struct {
	Session string      `json:"session"`
	Status  BatchStatus `json:"status"`
	Reason  string      `json:"reason,omitempty"`
	Result  *T          `json:"result,omitempty"`
	Err     error       `json:"-"`
	// A successful phase need not finish the target's whole operation.
	complete bool
}

type BatchReport[T any] struct {
	Results []BatchResult[T] `json:"results"`
}

// BatchError retains individual causes and child exit statuses. Presentation
// uses the corresponding report, so one failure never hides completed work.
type BatchError struct{ Causes []error }

func (e *BatchError) Error() string {
	return "Some target operations failed or were cancelled. Completed changes are retained."
}
func (e *BatchError) Unwrap() []error { return e.Causes }

// Selection and lock lifetimes belong to operation owners. The executor only
// tracks outcomes, including partial results across a multi-phase operation.
type batch[T any] struct {
	targets selectedTargets
	report  BatchReport[T]
}

func newBatch[T any](targets selectedTargets) *batch[T] {
	b := &batch[T]{targets: targets, report: BatchReport[T]{Results: make([]BatchResult[T], len(targets))}}
	for i, target := range targets {
		b.report.Results[i] = BatchResult[T]{Session: target.name, Err: target.err}
		if target.err != nil {
			b.report.Results[i].Status = BatchFailed
		}
	}
	return b
}

// Completed, failed or skipped targets never advance into later phases. A callback keeps
// its result payload even on failure, since earlier effects may have committed.
func (b *batch[T]) run(ctx context.Context, operation func(context.Context, SelectedTarget, *BatchResult[T]) error) {
	for i, target := range b.targets {
		item := &b.report.Results[i]
		if item.complete || item.Err != nil || item.Status == BatchSkipped {
			continue
		}
		if ctx.Err() != nil {
			item.Status, item.Reason = BatchSkipped, "Not completed: batch cancelled."
			continue
		}
		if err := operation(ctx, target, item); err != nil {
			item.Status, item.Err = BatchFailed, err
		} else if item.Status == "" {
			item.Status = BatchSucceeded
		}
	}
}

func (b *batch[T]) err(ctx context.Context) error {
	var failures []error
	for _, item := range b.report.Results {
		if item.Err != nil {
			failures = append(failures, item.Err)
		}
	}
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	if len(failures) > 0 {
		return &BatchError{Causes: failures}
	}
	return nil
}
