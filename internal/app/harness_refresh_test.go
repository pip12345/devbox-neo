package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/store"
)

func TestHarnessRefreshOnlyBypassesFinalStageCache(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, image := range []bool{false, true} {
			t.Run(fmt.Sprintf("custom=%t/image=%t", custom, image), func(t *testing.T) {
				e, d, q := fixture(t)
				if custom {
					for _, source := range q.Sources {
						write(t, filepath.Join(source.Path, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
					}
				}
				ctx := context.Background()
				made, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				before := sessionRecord(t, e, made.SessionID)
				start := len(d.History())
				request := recreateRequest(q)
				request.Options.RefreshHarness = true
				result, err := e.Recreate(ctx, request, image)
				if err != nil {
					t.Fatal(err)
				}
				var builds [][]string
				for _, args := range d.History()[start:] {
					if args[0] == "build" {
						builds = append(builds, args)
					}
				}
				want := 2
				if custom {
					want += 2*len(q.Sources) - 1
				}
				if len(builds) != want {
					t.Fatal("refresh reused the old image or skipped a captured stage", builds)
				}
				for i, args := range builds {
					if slices.Contains(args, "--no-cache") != (image || i == len(builds)-1) {
						t.Fatal("wrong stage cache policy", i, args)
					}
				}
				after := sessionRecord(t, e, made.SessionID)
				if before.ID != after.ID || before.Applied.ContainerID == after.Applied.ContainerID || before.Applied.ImageID == after.Applied.ImageID || !reflect.DeepEqual(before.Applied.Inputs, after.Applied.Inputs) {
					t.Fatal("refresh did not replace only the runtime association", before, after)
				}
				if len(result.Diagnostics) != 1 || !image && !strings.Contains(result.Diagnostics[0].Message, "Earlier image stages may use cache.") {
					t.Fatal("refresh did not explain its cache policy", result.Diagnostics)
				}
				buildCount := count(d, "build")
				request.Options.RefreshHarness = false
				request.Options.ForceContainer = true
				if _, err := e.Recreate(ctx, request, false); err != nil {
					t.Fatal(err)
				}
				if count(d, "build") != buildCount || sessionRecord(t, e, made.SessionID).Applied.ImageID != after.Applied.ImageID {
					t.Fatal("ordinary replacement did not retain the refreshed image")
				}
			})
		}
	}
}

func TestHarnessRefreshBuildFailureKeepsOldRuntime(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, made.Session, ""); err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, made.SessionID)
	failure := errors.New("harness installation failed")
	d.Fail = func(args []string) error {
		if args[0] == "build" && slices.Contains(args, "--no-cache") {
			return failure
		}
		return nil
	}
	request := recreateRequest(q)
	request.Options.RefreshHarness = true
	if _, err := e.Recreate(ctx, request, false); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if after := sessionRecord(t, e, made.SessionID); !reflect.DeepEqual(before, after) {
		t.Fatal("failed installation advanced the record")
	}
	if c, exists := sessionSnapshot(t, e, made.SessionID); !exists || c.ID != before.Applied.ContainerID || !c.State.Running {
		t.Fatal("failed installation changed the old runtime", c, exists)
	}
}

func TestHarnessRefreshRequiresIdleSession(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, made.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lock.Lease("test")
	lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	before := runtimeMutations(d)
	request := recreateRequest(q)
	request.Options.RefreshHarness = true
	_, singleErr := e.Recreate(ctx, request, false)
	_, bulkErr := e.RecreateAll(ctx, false, request.Options)
	for _, err := range []error{singleErr, bulkErr} {
		var busy *commanderror.Error
		if !errors.As(err, &busy) || busy.Code != "session_busy" {
			t.Fatal("harness refresh bypassed idle checks", err)
		}
	}
	if runtimeMutations(d) != before {
		t.Fatal("busy harness refresh mutated runtime")
	}
}

func TestHarnessRefreshAllPreservesHistoryAndLifetime(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	var records []store.Record
	for _, name := range []string{"stopped", "running", "missing"} {
		q.LocalName = name
		made, err := e.Create(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if name == "running" {
			if _, err := e.Start(ctx, made.Session, ""); err != nil {
				t.Fatal(err)
			}
		}
		r := sessionRecord(t, e, made.SessionID)
		write(t, filepath.Join(e.Store.Home, "sessions", r.Directory, r.StateTree, "harnesses/pi/stores/home/history"), name)
		if name == "missing" {
			forgetSession(t, e, r.ID)
		}
		records = append(records, r)
	}
	builds := count(d, "build")
	applied, err := e.RecreateAll(ctx, false, RecreateOptions{RefreshHarness: true})
	if err != nil || len(applied) != len(records) || count(d, "build") != builds+2*len(records) {
		t.Fatal("bulk refresh did not build for all selected sessions", applied, err)
	}
	for _, before := range records {
		after := sessionRecord(t, e, before.ID)
		c, exists := sessionSnapshot(t, e, before.ID)
		if !exists || c.ID == before.Applied.ContainerID || c.State.Running != before.Settings.ManualStart || !reflect.DeepEqual(before.Settings, after.Settings) || !before.Created.Equal(after.Created) {
			t.Fatal("bulk refresh lost identity or lifetime", before.Settings.LocalName, c, after)
		}
		if string(getFile(t, filepath.Join(e.Store.Home, "sessions", before.Directory, before.StateTree, "harnesses/pi/stores/home/history"))) != before.Settings.LocalName {
			t.Fatal("bulk refresh changed saved history")
		}
	}
}
