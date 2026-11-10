package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"devbox/internal/docker"
	"devbox/internal/store"
)

func successfulSessions[T any](report BatchReport[T]) []string {
	var sessions []string
	for _, item := range report.Results {
		if item.Status == BatchSucceeded {
			sessions = append(sessions, item.Session)
		}
	}
	return sessions
}

func TestRecreateAllContinuesAfterExecutionFailures(t *testing.T) {
	for _, failName := range []string{"a", "b", "c"} {
		t.Run(failName, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			var records []store.Record
			for _, name := range []string{"a", "b", "c"} {
				q.LocalName = name
				made, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, sessionRecord(t, e, made.SessionID))
			}
			cause := &docker.ExitError{Operation: "rm", Code: 31}
			d.Fail = func(args []string) error {
				for _, r := range records {
					if r.Settings.LocalName == failName && slices.Equal(args, []string{"rm", r.Applied.ContainerID}) {
						return cause
					}
				}
				return nil
			}
			report, err := e.RecreateAll(ctx, false, RecreateOptions{ForceContainer: true})
			if !errors.Is(err, cause) || len(report.Results) != 3 || len(successfulSessions(report)) != 2 {
				t.Fatal("execution failure stopped independent sessions or lost its cause", report, err)
			}
			for _, r := range records {
				after := sessionRecord(t, e, r.ID)
				if (after.Applied.ContainerID == r.Applied.ContainerID) != (r.Settings.LocalName == failName) {
					t.Fatal("wrong session replaced", r.Settings.LocalName)
				}
				if r.Settings.LocalName == failName && !reflect.DeepEqual(r, after) {
					t.Fatal("failure before removal advanced the record")
				}
			}
		})
	}
}

func TestSessionBatchCancellationStopsCallbacksAndReleasesLocks(t *testing.T) {
	e, _, q := fixture(t)
	var records []store.Record
	for _, name := range []string{"a", "b", "c"} {
		q.LocalName = name
		made, err := e.Create(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, sessionRecord(t, e, made.SessionID))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	targets, err := e.selectSavedSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := newBatchEntries[Result](targets)
	runBatch(ctx, items, func(ctx context.Context, item *batchEntry[Result]) error {
		lock, r, err := e.lockSelectedSession(ctx, item.target)
		if err != nil {
			return err
		}
		defer lock.Close()
		calls++
		if !lock.Held() || lock.ID != r.ID {
			t.Fatal("callback did not own the selected session lock")
		}
		cancel()
		return nil
	})
	report, err := reportBatch(ctx, items)
	if !errors.Is(err, context.Canceled) || calls != 1 || len(report.Results) != 3 || len(successfulSessions(report)) != 1 {
		t.Fatal("cancellation lost completed work or continued processing", report, err, calls)
	}
	for _, item := range report.Results[1:] {
		if item.Status != BatchSkipped || item.Reason == "" {
			t.Fatal("cancelled work was not explicitly reported", item)
		}
	}
	for _, r := range records {
		check, done := context.WithTimeout(context.Background(), time.Second)
		lock, err := e.Store.Lock(check, r.ID)
		if err != nil {
			done()
			t.Fatal("batch leaked a session lock", err)
		}
		lock.Close()
		done()
	}
}

func TestBatchRunsSuppliedWorkAndRetainsPartialResults(t *testing.T) {
	cause := errors.New("phase failed")
	selectionErr := errors.New("selection failed")
	items := newBatchEntries[int](selectedTargets{
		{target: SelectedTarget{name: "a"}}, {target: SelectedTarget{name: "b"}},
		{target: SelectedTarget{name: "c"}}, {target: SelectedTarget{name: "invalid"}, err: selectionErr},
	})
	ctx := context.Background()
	runBatch(ctx, items, func(_ context.Context, item *batchEntry[int]) error {
		if item.target.name == "invalid" {
			t.Fatal("executed failed selection")
		}
		item.outcome.Result = new(int)
		*item.outcome.Result = 1
		return nil
	})
	runBatch(ctx, items, func(_ context.Context, item *batchEntry[int]) error {
		*item.outcome.Result++
		if item.target.name == "b" {
			return cause
		}
		return nil
	})
	// The owner supplies only work still eligible for another phase.
	runBatch(ctx, items[1:3], func(_ context.Context, item *batchEntry[int]) error {
		if item.target.name != "c" {
			t.Fatal("executed omitted or failed work", item.target.name)
		}
		*item.outcome.Result++
		return nil
	})
	report, err := reportBatch(ctx, items)
	if !errors.Is(err, cause) || !errors.Is(err, selectionErr) {
		t.Fatal(err)
	}
	if *report.Results[0].Result != 2 || *report.Results[1].Result != 2 || *report.Results[2].Result != 3 {
		t.Fatal("phase transition lost partial results", report)
	}
}

func TestSessionBatchDoesNotExecuteWithUnavailableInventory(t *testing.T) {
	e, d, q := fixture(t)
	if _, err := e.Create(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(e.Store.Home, "sessions")
	if err := os.Rename(root, root+".saved"); err != nil {
		t.Fatal(err)
	}
	write(t, root, "not a directory")
	firstCall := len(d.History())
	report, err := e.RecreateAll(context.Background(), false, RecreateOptions{})
	if err == nil || len(report.Results) != 0 || len(d.History()) != firstCall {
		t.Fatal("batch executed without a trustworthy selection", report, err)
	}
}
