package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runtimeMutations(d interface{ History() [][]string }) int {
	n := 0
	for _, a := range d.History() {
		switch a[0] {
		case "build", "create", "start", "stop", "rm", "cp", "exec", "update":
			n++
		}
	}
	return n
}

func TestApplicationPlanPrecedesRuntimeMutations(t *testing.T) {
	for _, change := range []string{"runtime", "container", "image"} {
		t.Run(change, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			source := q.Sources[0].Path
			write(t, filepath.Join(source, "pi/new-file"), "new")
			if change == "container" {
				write(t, filepath.Join(source, "config.json"), `{"harness":"pi","network":"host"}`)
			}
			if change == "image" {
				write(t, filepath.Join(source, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
			}
			r := sessionRecord(t, e, made.SessionID)
			live := filepath.Join(e.Store.Home, "sessions", r.Directory, "harnesses/pi/stores/home/new-file")
			before, plans := runtimeMutations(d), 0
			e.OnDiagnostic = func(diagnostic Diagnostic) {
				if diagnostic.Code != "apply_plan" {
					t.Fatal(diagnostic)
				}
				if runtimeMutations(d) != before {
					t.Fatal("mutated before explanation")
				}
				if _, err := os.Stat(live); !os.IsNotExist(err) {
					t.Fatal("synced before explanation", err)
				}
				if change != "runtime" && !strings.Contains(diagnostic.Message, "Container-local changes will be lost") {
					t.Fatal("missing replacement warning")
				}
				plans++
			}
			result, err := e.Recreate(ctx, recreateRequest(q), false)
			if err != nil || plans != 1 || len(result.Diagnostics) != 1 {
				t.Fatal(result, err)
			}
		})
	}
}

func TestCancelledApplicationPlanDoesNotMutate(t *testing.T) {
	e, d, q := fixture(t)
	made, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(q.Sources[0].Path, "config.json"), `{"harness":"pi","network":"host"}`)
	r := sessionRecord(t, e, made.SessionID)
	path, _ := e.Store.RecordPath(r.Directory)
	beforeRecord, before := string(getFile(t, path)), runtimeMutations(d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.OnDiagnostic = func(Diagnostic) { cancel() }
	if _, err := e.Recreate(ctx, recreateRequest(q), false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if runtimeMutations(d) != before || string(getFile(t, path)) != beforeRecord {
		t.Fatal("cancelled apply changed runtime or record")
	}
	lockCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	lock, err := e.Store.Lock(lockCtx, r.Directory, r.ID)
	if err != nil {
		t.Fatal("apply leaked lock", err)
	}
	lock.Close()
}
