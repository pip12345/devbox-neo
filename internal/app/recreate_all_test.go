package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/store"
)

func TestRecreateAllIncludesMissingRuntimeAndPreservesSessionState(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	var records []store.Record
	for _, name := range []string{"stopped", "running", "missing", "missing-manual"} {
		q.LocalName = name
		made, err := e.Create(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if name == "running" || name == "missing-manual" {
			if _, err := e.Start(ctx, made.Session, ""); err != nil {
				t.Fatal(err)
			}
		}
		r := sessionRecord(t, e, made.SessionID)
		write(t, filepath.Join(e.Store.Home, "sessions", r.Directory, r.StateTree, "harnesses/pi/stores/home/history"), name)
		if strings.HasPrefix(name, "missing") {
			forgetSession(t, e, r.ID)
		}
		if name == "missing" {
			if err := e.SetDefault(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
		records = append(records, r)
	}
	builds := count(d, "build")
	report, err := e.RecreateAll(ctx, false, RecreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	applied := successfulSessions(report)
	sort.Slice(records, func(i, j int) bool { return records[i].Directory < records[j].Directory })
	var want []string
	for _, before := range records {
		want = append(want, before.PublicName())
		after := sessionRecord(t, e, before.ID)
		c, exists := sessionSnapshot(t, e, before.ID)
		if !exists || c.State.Running != (before.Settings.LocalName == "running" || before.Settings.ManualStart) {
			t.Fatal("bulk recreation lost runtime intent", before.Settings.LocalName, c, exists)
		}
		if after.ID != before.ID || after.Directory != before.Directory || !after.Created.Equal(before.Created) || !reflect.DeepEqual(after.Settings, before.Settings) {
			t.Fatal("bulk recreation changed saved session identity/settings", after)
		}
		missing := strings.HasPrefix(before.Settings.LocalName, "missing")
		if (after.Applied.ContainerID != before.Applied.ContainerID) != missing {
			t.Fatal("bulk recreation did not choose minimal work", before.Settings.LocalName)
		}
		if string(getFile(t, filepath.Join(e.Store.Home, "sessions", before.Directory, before.StateTree, "harnesses/pi/stores/home/history"))) != before.Settings.LocalName {
			t.Fatal("bulk recreation changed saved history")
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(applied, want) || count(d, "build") != builds {
		t.Fatal("bulk recreation missed sessions or rebuilt compatible images", applied, want)
	}
	selected, err := e.Store.ReadDefault(ctx, q.Workspace)
	if err != nil || selected == nil || sessionRecord(t, e, selected.ID).Settings.LocalName != "missing" {
		t.Fatal("bulk recreation changed the folder default", selected, err)
	}
}

func TestRecreateAllContinuesAfterPerSessionPreflightFailures(t *testing.T) {
	for _, issue := range []string{"corrupt", "duplicate identity", "missing store", "active command"} {
		t.Run(issue, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			var records []store.Record
			for _, name := range []string{"first", "second"} {
				q.LocalName = name
				made, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, sessionRecord(t, e, made.SessionID))
				forgetSession(t, e, made.SessionID)
			}
			sort.Slice(records, func(i, j int) bool { return records[i].Directory < records[j].Directory })
			broken := records[1]
			path, err := e.Store.RecordPath(broken.Directory)
			if err != nil {
				t.Fatal(err)
			}
			switch issue {
			case "corrupt":
				write(t, path, "broken")
			case "duplicate identity":
				broken.ID = records[0].ID
				broken.Applied.ImageTag = records[0].Applied.ImageTag
				data, err := json.Marshal(broken)
				if err != nil {
					t.Fatal(err)
				}
				write(t, path, string(data))
			case "missing store":
				if err := os.RemoveAll(filepath.Join(e.Store.Home, "sessions", broken.Directory, broken.StateTree, "harnesses/pi/stores/home")); err != nil {
					t.Fatal(err)
				}
			case "active command":
				lock, err := e.Store.Lock(ctx, broken.ID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = lock.Lease("test")
				lock.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			report, err := e.RecreateAll(ctx, false, RecreateOptions{})
			if err == nil || len(report.Results) != 2 || len(successfulSessions(report)) != 1 {
				t.Fatal("invalid saved session blocked independent work or disappeared from results", report, err)
			}
			if _, exists := sessionSnapshot(t, e, records[0].ID); !exists {
				t.Fatal("healthy session was not recreated")
			}
			if _, exists := d.Snapshot(broken.Applied.Creation.Name); exists {
				t.Fatal("failed session was recreated")
			}
		})
	}
}

func TestRecreateAllReportsPendingTransferAndContinues(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			return errors.New("interrupted destination build")
		}
		return nil
	}
	if _, err := e.Transfer(ctx, TransferOptions{Source: made.Session, Destination: q.Workspace, As: "copy", Mode: "clone"}); err == nil {
		t.Fatal("transfer failure injection did not leave pending work")
	}
	d.Fail = nil
	q.LocalName = "independent"
	independent, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	forgetSession(t, e, independent.SessionID)
	report, err := e.RecreateAll(ctx, false, RecreateOptions{})
	var pending *commanderror.Error
	if !errors.As(err, &pending) || pending.Code != "pending_transfer" || !reflect.DeepEqual(successfulSessions(report), []string{independent.Session}) {
		t.Fatal("pending transfer blocked unrelated sessions or was not reported", report, err)
	}
	if _, exists := sessionSnapshot(t, e, independent.SessionID); !exists {
		t.Fatal("independent session was not recreated")
	}
}

func TestRecreateAllDoesNotAdoptContainersOrIncompleteAllocations(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	c, exists := sessionSnapshot(t, e, made.SessionID)
	if !exists {
		t.Fatal("fixture container is absent")
	}
	c.ID, c.Name = strings.Repeat("f", 64), "/dbx-unmatched"
	c.Config.Labels = docker.Owner{Installation: e.Store.Installation, Session: strings.Repeat("f", 32), Workspace: q.Workspace, LocalName: "unmatched"}.Labels()
	d.SetContainer(c)
	marker := filepath.Join(e.Store.Home, "sessions/dbx-incomplete/marker")
	write(t, marker, "retain")
	applied, err := e.RecreateAll(ctx, false, RecreateOptions{})
	if err != nil || !reflect.DeepEqual(successfulSessions(applied), []string{made.Session}) {
		t.Fatal("bulk recreation selected something other than saved sessions", applied, err)
	}
	if got, exists := d.Snapshot("dbx-unmatched"); !exists || !reflect.DeepEqual(got, c) || string(getFile(t, marker)) != "retain" {
		t.Fatal("bulk recreation changed unmatched runtime or incomplete state")
	}
}
