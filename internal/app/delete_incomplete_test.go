package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/environment"
	"devbox/internal/fsutil"
	"devbox/internal/store"
)

func incompleteFixture(t *testing.T, e *Engine) (string, string) {
	t.Helper()
	name := "dbx-42643868833d.main"
	root := filepath.Join(e.Store.Home, "sessions", name)
	write(t, filepath.Join(root, "harnesses/pi/stores/home/extensions-pip/copied-file"), "keep until confirmed")
	if err := os.MkdirAll(filepath.Join(root, "runtime/ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	return name, root
}

func TestDeleteIncompleteCreationScopeAndConfirmation(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		scope   DeleteScope
		dryRun  bool
		answer  *bool
		deleted bool
	}{
		{name: "container retains directory", scope: DeleteContainer},
		{name: "whole session removes directory", scope: DeleteSession, deleted: true},
		{name: "preview", scope: DeleteSession, dryRun: true},
		{name: "confirm data", scope: DeleteSession, answer: &yes, deleted: true},
		{name: "decline data", scope: DeleteSession, answer: &no},
		{name: "unscoped confirm", answer: &yes, deleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, d, _ := fixture(t)
			name, root := incompleteFixture(t, e)
			ctx := context.Background()
			report, err := e.List(ctx, "")
			if err != nil || len(report.Sessions) != 1 || !report.Sessions[0].Uncommitted {
				t.Fatal("missing interrupted-creation row", report, err)
			}
			options := DeleteOptions{Selection: Selection{Targets: []string{name, name}}, Scope: tc.scope, DryRun: tc.dryRun}
			prompts := 0
			if tc.answer != nil {
				options.Confirm = func(prompt DeletePrompt) (bool, error) {
					prompts++
					if len(prompt.Sessions)+len(prompt.Containers) != 0 || !reflect.DeepEqual(prompt.IncompleteDirectories, []string{name}) {
						t.Fatal("invented a session/container", prompt)
					}
					return *tc.answer, nil
				}
			}
			result, err := e.Delete(ctx, options)
			if err != nil {
				t.Fatal(result, err)
			}
			if len(result.Targets) != 1 || result.Targets[0].Name() != name || result.Targets[0].sessionID != "" || len(result.Sessions)+len(result.Containers) != 0 {
				t.Fatal("incomplete directory received a fabricated identity", result)
			}
			planned := tc.deleted || tc.dryRun
			if (len(result.IncompleteDirectories) == 1) != planned || (len(result.RetainedIncompleteDirectories) == 1) == planned {
				t.Fatal("wrong receipt", result)
			}
			if (prompts == 1) != (tc.answer != nil) {
				t.Fatal("wrong confirmation count", prompts)
			}
			_, err = os.Stat(root)
			if os.IsNotExist(err) != tc.deleted {
				t.Fatal("wrong directory outcome", err)
			}
			for _, args := range d.History() {
				if args[0] != "container" {
					t.Fatal("cleanup mutated or inferred Docker ownership", args)
				}
			}
		})
	}
}

func TestDeleteIncompleteCreationProtectsMountedState(t *testing.T) {
	for _, sourceKind := range []string{"child", "root", "ancestor", "symlink", "sibling", "named volume"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(sourceKind+map[bool]string{false: " execute", true: " preview"}[dryRun], func(t *testing.T) {
				e, d, _ := fixture(t)
				name, root := incompleteFixture(t, e)
				source := root
				kind := "bind"
				blocked := true
				switch sourceKind {
				case "child":
					source = filepath.Join(root, "harnesses/pi/stores/home")
				case "ancestor":
					source = e.Store.Home
				case "symlink":
					source = filepath.Join(t.TempDir(), "alias")
					if err := os.Symlink(root, source); err != nil {
						t.Fatal(err)
					}
				case "sibling":
					source, blocked = root+"-other", false
				case "named volume":
					kind, blocked = "volume", false
				}
				// Unmanaged, stopped containers still prevent removal of their
				// backing files. Force is not permission to break those mounts.
				c := docker.Container{ID: strings.Repeat("a", 64), Name: "/unmanaged", Mounts: []docker.ContainerMount{{Type: kind, Source: source, Destination: "/data"}}}
				d.SetContainer(c)
				result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession, Force: true, DryRun: dryRun})
				var failure *commanderror.Error
				if blocked {
					if !errors.As(err, &failure) || failure.Code != "incomplete_directory_in_use" || result.DeletedCount() != 0 {
						t.Fatal(result, err)
					}
					if _, err := os.Stat(root); err != nil {
						t.Fatal(err)
					}
				} else if err != nil || len(result.IncompleteDirectories) != 1 {
					t.Fatal(result, err)
				}
			})
		}
	}
}

func TestDeleteIncompleteCreationRechecksAfterConfirmation(t *testing.T) {
	for _, change := range []string{"record", "replacement", "mounted", "cancel"} {
		t.Run(change, func(t *testing.T) {
			e, d, _ := fixture(t)
			name, root := incompleteFixture(t, e)
			result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession, Confirm: func(DeletePrompt) (bool, error) {
				switch change {
				case "record":
					write(t, filepath.Join(root, "session.json"), "not a valid record")
				case "replacement":
					if err := os.Rename(root, root+"-old"); err != nil {
						t.Fatal(err)
					}
					write(t, filepath.Join(root, "new-owner"), "do not delete")
				case "mounted":
					d.SetContainer(docker.Container{ID: strings.Repeat("b", 64), Name: "/new-container", Mounts: []docker.ContainerMount{{Type: "bind", Source: root}}})
				case "cancel":
					return false, context.Canceled
				}
				return true, nil
			}})
			if err == nil || result.DeletedCount() != 0 {
				t.Fatal("changed state was deleted", result, err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteIncompleteCreationKeepsNamespaceLock(t *testing.T) {
	e, _, _ := fixture(t)
	name, _ := incompleteFixture(t, e)
	_, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession, Confirm: func(DeletePrompt) (bool, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		lock, err := e.Store.LockNames(ctx)
		if err == nil {
			fsutil.Unlock(lock)
			t.Fatal("creation could acquire namespace during confirmation")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		return true, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeleteIncompleteCreationWaitsForCreationNamespace(t *testing.T) {
	e, _, _ := fixture(t)
	name, root := incompleteFixture(t, e)
	lock, err := e.Store.LockNames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer fsutil.Unlock(lock)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteIncompleteCreationDoesNotEnterBulkSelection(t *testing.T) {
	for _, selection := range []Selection{{All: true}, {Stopped: true}} {
		e, _, _ := fixture(t)
		_, root := incompleteFixture(t, e)
		result, err := e.Delete(context.Background(), DeleteOptions{Selection: selection, Scope: DeleteSession})
		if err != nil || result.DeletedCount() != 0 {
			t.Fatal(result, err)
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeleteIncompleteCreationMixedPreflight(t *testing.T) {
	e, d, q := fixture(t)
	created, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	name, root := incompleteFixture(t, e)
	d.SetContainer(docker.Container{ID: strings.Repeat("c", 64), Name: "/foreign", Mounts: []docker.ContainerMount{{Type: "bind", Source: root}}})
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{created.SessionID, name}}, Scope: DeleteSession})
	if err == nil || result.DeletedCount() != 0 {
		t.Fatal(result, err)
	}
	if _, exists := sessionSnapshot(t, e, created.SessionID); !exists {
		t.Fatal("removed valid container before incomplete preflight")
	}
	delete(d.Containers, "foreign")
	result, err = e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{created.SessionID, name}}, Scope: DeleteSession})
	if err != nil || result.DeletedCount() != 3 || len(result.Retained)+len(result.RetainedIncompleteDirectories) != 0 {
		t.Fatal(result, err)
	}
}

func TestDeleteIncompleteCreationRejectsPendingTransfer(t *testing.T) {
	e, _, q := fixture(t)
	ctx := context.Background()
	created, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, created.SessionID)
	name, root := incompleteFixture(t, e)
	id, _ := fsutil.ID()
	nonce, _ := fsutil.ID()
	j := store.Transfer{Version: 4, ContainerName: environment.ResourceName(q.Workspace, "copy", nonce), SourceContainerID: source.Applied.SetupContainer, ID: nonce, Mode: "clone", Phase: "prepare", Source: environment.Identity{Binding: source.Settings.Binding, Name: source.Directory}, Destination: environment.Identity{Binding: environment.Binding{Workspace: q.Workspace, LocalName: "copy"}, Name: name}, SourceID: source.ID, DestinationID: id, Started: time.Now().UTC()}
	names := []string{source.Directory, name}
	sort.Strings(names)
	locks, err := e.Store.LockAll(ctx, names, map[string]string{source.Directory: source.ID, name: id})
	if err != nil {
		t.Fatal(err)
	}
	a, b := locks[0], locks[1]
	if a.Name != source.Directory {
		a, b = b, a
	}
	err = a.SaveTransfer(b, j)
	store.CloseAll(locks)
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.Delete(ctx, DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession, Force: true})
	var failure *commanderror.Error
	if !errors.As(err, &failure) || failure.Code != "pending_transfer" || result.DeletedCount() != 0 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteIncompleteCreationRejectsAmbiguousContainerName(t *testing.T) {
	e, d, _ := fixture(t)
	name, root := incompleteFixture(t, e)
	d.SetContainer(docker.Container{ID: strings.Repeat("d", 64), Name: "/" + name})
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{name}}, Scope: DeleteSession})
	if err == nil || !strings.Contains(err.Error(), "both an incomplete directory and a container") || result.DeletedCount() != 0 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
	if _, exists := d.Snapshot(name); !exists {
		t.Fatal("same-named container deleted")
	}
}

func TestDeleteIncompleteCreationInspectionFailurePreventsMutation(t *testing.T) {
	e, d, q := fixture(t)
	created, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	name, root := incompleteFixture(t, e)
	failure := errors.New("Docker inventory failed")
	d.Fail = func(args []string) error {
		if slices.Equal(args, []string{"container", "ls", "--all", "--no-trunc", "--format", "{{.ID}}"}) {
			return failure
		}
		return nil
	}
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{name, created.SessionID}}, Scope: DeleteSession})
	if !errors.Is(err, failure) || result.DeletedCount() != 0 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
	if _, exists := sessionSnapshot(t, e, created.SessionID); !exists {
		t.Fatal("valid container deleted before inventory check")
	}
}

func TestDeleteIncompleteCreationDoesNotRemoveUnverifiedImage(t *testing.T) {
	e, d, q := fixture(t)
	created, err := e.Create(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := sessionRecord(t, e, created.SessionID)
	if _, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{created.SessionID}}, Scope: DeleteContainer}); err != nil {
		t.Fatal(err)
	}
	p, _ := e.Store.RecordPath(r.Directory)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	result, err := e.Delete(context.Background(), DeleteOptions{Selection: Selection{Targets: []string{r.Directory}}, Scope: DeleteSession})
	if err != nil || !slices.Equal(result.IncompleteDirectories, []string{r.Directory}) {
		t.Fatal(result, err)
	}
	if _, exists := d.Images[r.Applied.ImageTag]; !exists {
		t.Fatal("guessed image association from incomplete directory")
	}
}
