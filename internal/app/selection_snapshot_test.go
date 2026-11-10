package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCapturedDeletionKeepsSessionAfterDefaultChanges(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	first, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	chosen := sessionRecord(t, e, first.SessionID)
	if err := e.SetDefault(ctx, chosen); err != nil {
		t.Fatal(err)
	}
	q.LocalName = "second"
	second, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{q.Workspace}}, Scope: DeleteContainer, DryRun: true})
	if err != nil || len(preview.Targets) != 1 || preview.Targets[0].Name() != chosen.PublicName() {
		t.Fatal(preview, err)
	}
	if err := e.SetDefault(ctx, sessionRecord(t, e, second.SessionID)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteContainer}); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot(chosen.Applied.Creation.Name); exists {
		t.Fatal("captured session was not removed")
	}
	if _, exists := sessionSnapshot(t, e, second.SessionID); !exists {
		t.Fatal("changed default retargeted deletion")
	}
	if current, err := e.Locate(ctx, q.Workspace, ""); err != nil || current.ID != second.SessionID {
		t.Fatal("container deletion changed default selection", current, err)
	}
}

func TestSelectionReturnsFactsWithoutMutatingRequest(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	request := Selection{Targets: []string{r.Directory, r.Directory}}
	for range 2 {
		targets, err := e.selectContainers(ctx, request)
		if err != nil || len(targets) != 1 || targets[0].target.sessionID != r.ID || targets[0].target.record != recordPresent {
			t.Fatal(targets, err)
		}
		if len(request.Targets) != 2 || len(request.Captured) != 0 {
			t.Fatal("discovery mutated its request", request)
		}
	}
}

func TestCapturedOrphanRejectsReplacementRuntime(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, made.SessionID)
	path, _ := e.Store.RecordPath(r.Directory)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{ContainerNames: []string{r.Applied.Creation.Name}}, Scope: DeleteContainer, DryRun: true})
	if err != nil || len(preview.Targets) != 1 {
		t.Fatal(preview, err)
	}
	if preview.Targets[0].sessionID != r.ID || !preview.Targets[0].unmatched {
		t.Fatal("orphan lost its lock owner", preview.Targets)
	}
	c, _ := d.Snapshot(r.Applied.Creation.Name)
	c.ID = strings.Repeat("b", 64)
	d.Containers[r.Applied.Creation.Name] = c
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteContainer}); err == nil {
		t.Fatal("captured orphan adopted a replacement runtime")
	}
	if _, exists := d.Snapshot(r.Applied.Creation.Name); !exists {
		t.Fatal("replacement runtime was deleted")
	}
}

func TestCapturedRuntimeFailureProtectsSharedOwnerState(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "inspection failure", true: "replacement"}[replacement], func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			made, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			r := sessionRecord(t, e, made.SessionID)
			extra, _ := sessionSnapshot(t, e, r.ID)
			extra.ID, extra.Name = strings.Repeat("e", 64), "/extra-owned"
			d.SetContainer(extra)
			preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{r.ID}, ContainerNames: []string{"extra-owned"}}, Scope: DeleteContainer, DryRun: true})
			if err != nil || len(preview.Targets) != 2 {
				t.Fatal(preview, err)
			}
			cause := errors.New("temporary extra-container inspection failure")
			if replacement {
				extra.ID = strings.Repeat("f", 64)
				d.SetContainer(extra)
			} else {
				d.Fail = func(args []string) error {
					if slices.Equal(args, []string{"container", "inspect", extra.ID}) {
						return cause
					}
					return nil
				}
			}
			result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteSession})
			if err == nil || !replacement && !errors.Is(err, cause) || len(result.Targets) != 2 || len(result.Sessions) != 0 || !slices.Equal(result.Retained, []string{r.PublicName()}) {
				t.Fatal("captured lookup failure lost the owner and exposed saved state", result, err)
			}
			if _, err := e.Store.Read(ctx, r.ID); err != nil {
				t.Fatal("saved state was removed under the failed runtime", err)
			}
			if _, exists := d.Snapshot("extra-owned"); !exists {
				t.Fatal("failed runtime was removed")
			}
			for _, target := range result.Targets {
				if target.Name() == "extra-owned" && (target.sessionID != r.ID || target.containerID != strings.Repeat("e", 64)) {
					t.Fatal("failed revalidation changed captured ownership or instance", target)
				}
			}
		})
	}
}

func TestCapturedLockFailureCanRetryAfterRepair(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Store.Home, "state", "locks", "sessions", made.SessionID+".lock")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	preview, previewErr := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{made.SessionID}}, Scope: DeleteContainer, DryRun: true})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if previewErr == nil || len(preview.Targets) != 1 {
		t.Fatal(preview, previewErr)
	}
	result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteContainer})
	if err != nil || len(result.Containers) != 1 {
		t.Fatal("repaired lock failure stayed cached in the snapshot", result, err)
	}
}

func TestUnresolvedSelectionIsNotCapturedAsAuthority(t *testing.T) {
	e, _, _ := fixture(t)
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{strings.Repeat("f", 12)}}, Scope: DeleteContainer, DryRun: true})
	if err == nil || len(result.Batch.Results) != 1 || len(result.Targets) != 0 {
		t.Fatal("failed lookup became a reusable identity snapshot", result, err)
	}
}

func TestCapturedIncompleteDeletionRetainsDirectoryIdentity(t *testing.T) {
	e, _, _ := fixture(t)
	ctx := context.Background()
	name := strings.Repeat("f", 12)
	root := filepath.Join(e.Store.Home, "sessions", name)
	write(t, filepath.Join(root, "marker"), "original")
	preview, err := e.Delete(ctx, DeleteOptions{Selection: Selection{IncompleteDirs: []string{name}}, Scope: DeleteSession, DryRun: true})
	if err != nil || len(preview.Targets) != 1 {
		t.Fatal(preview, err)
	}
	if err := os.Rename(root, root+"-retained"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "marker"), "replacement")
	if _, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Captured: preview.Targets}, Scope: DeleteSession}); err == nil {
		t.Fatal("captured incomplete directory adopted replacement files")
	}
	if got := getFile(t, filepath.Join(root, "marker")); string(got) != "replacement" {
		t.Fatal("replacement files changed")
	}
}
