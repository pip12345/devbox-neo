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

type batchEntry[T any] struct {
	target  SelectedTarget
	outcome BatchResult[T]
}

func newBatchEntry[T any](selection targetSelection) batchEntry[T] {
	return batchEntry[T]{target: selection.target, outcome: BatchResult[T]{Session: selection.target.name, Err: selection.err}}
}

func newBatchEntries[T any](selections selectedTargets) []*batchEntry[T] {
	items := make([]*batchEntry[T], 0, len(selections))
	for _, selection := range selections {
		item := newBatchEntry[T](selection)
		items = append(items, &item)
	}
	return items
}

func (e *batchEntry[T]) batchOutcome() *BatchResult[T] { return &e.outcome }

type batchItem[T any] interface {
	batchOutcome() *BatchResult[T]
}

// Operation owners supply the eligible work for each phase. The executor does
// not own phase progress or infer completion from a successful callback.
func runBatch[T any, I batchItem[T]](ctx context.Context, items []I, operation func(context.Context, I) error) {
	for _, item := range items {
		outcome := item.batchOutcome()
		if outcome.Err != nil || outcome.Status == BatchSkipped {
			continue
		}
		if ctx.Err() != nil {
			outcome.Status, outcome.Reason = BatchSkipped, "Not completed: batch cancelled."
			continue
		}
		outcome.Err = operation(ctx, item)
	}
}

func reportBatch[T any, I batchItem[T]](ctx context.Context, items []I) (BatchReport[T], error) {
	report := BatchReport[T]{Results: make([]BatchResult[T], 0, len(items))}
	var failures []error
	for _, item := range items {
		outcome := *item.batchOutcome()
		if outcome.Err != nil {
			outcome.Status = BatchFailed
			failures = append(failures, outcome.Err)
		} else if outcome.Status == "" {
			outcome.Status = BatchSucceeded
		}
		report.Results = append(report.Results, outcome)
	}
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	if len(failures) > 0 {
		return report, &BatchError{Causes: failures}
	}
	return report, nil
}
