package migration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Linux access events prove payloads were not opened/walked, without flaky
// duration thresholds, disk-cache assumptions, or multi-gigabyte test fixtures.
func watchPayloadReads(t *testing.T, paths ...string) func() bool {
	t.Helper()
	fd, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	for _, path := range paths {
		if _, err := unix.InotifyAddWatch(fd, path, unix.IN_OPEN|unix.IN_ACCESS); err != nil {
			t.Fatal(err)
		}
	}
	return func() bool {
		t.Helper()
		var data [4096]byte
		n, err := unix.Read(fd, data[:])
		if errors.Is(err, unix.EAGAIN) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
}

func TestDiscoveryDoesNotReadOrWalkPayloads(t *testing.T) {
	p, name := fixture(t)
	cache := filepath.Join(p.Source, "cache/harnesses/pi/npm-cache")
	global := filepath.Join(p.Source, "cache/harnesses/pi/npm-global")
	put(t, filepath.Join(cache, "nested/package"), "cached-private")
	put(t, filepath.Join(global, "node_modules/tool/index.js"), "installed-private")
	readPayload := watchPayloadReads(t, cache, global,
		filepath.Join(p.Source, "profiles/work/inputs"),
		filepath.Join(p.Source, "sessions", name, "pi"),
		filepath.Join(p.Source, "auth/pi/auth.json"))
	v := inventory(t, p)
	if readPayload() {
		t.Fatal("discovery opened a payload file or directory")
	}
	for _, item := range v.Items {
		if item.Scanned {
			t.Fatal("discovery claims a data snapshot")
		}
	}
	for _, file := range v.Files {
		if file.Data == nil {
			t.Fatal("discovery included payload files")
		}
	}
	var report bytes.Buffer
	if err := Report(&report, v, nil); err != nil {
		t.Fatal(err)
	}
	requireText(t, report.String(), "Metadata only", "Portable data: not scanned", "deferred until staging selection")
	if strings.Contains(report.String(), "Portable bytes:") {
		t.Fatal("discovery invents byte totals")
	}
	absent(t, p.Work)
	absent(t, filepath.Join(p.Source, "state/locks"))
}

func TestExcludedPayloadsAreNotScannedDuringStageOrResume(t *testing.T) {
	p, _ := fixture(t)
	workspace := filepath.Join(filepath.Dir(p.Source), "skipped-workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	skipped := addSession(t, p, workspace, "work", "pi", strings.Repeat("c", 32), false)
	sessionData := filepath.Join(p.Source, "sessions", skipped, "pi")
	cache := filepath.Join(p.Source, "cache/harnesses/pi/npm-cache")
	put(t, filepath.Join(cache, "nested/package"), "cache")
	// Deep unsupported entries in unselected owners must not be walked either.
	if err := unix.Mkfifo(filepath.Join(cache, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	readPayload := watchPayloadReads(t, cache, sessionData)
	v := inventory(t, p)
	var progress bytes.Buffer
	j, err := Stage(t.Context(), v, Selection{Skip: []string{"session:" + skipped}}, &fakeSource{check: func(n int) error {
		if n == 2 {
			return errors.New("interrupted")
		}
		return nil
	}}, &progress)
	if err == nil || j == nil {
		t.Fatal("expected interrupted stage", err)
	}
	if readPayload() {
		t.Fatal("stage scanned an excluded payload")
	}
	for _, file := range j.Inventory.Files {
		if file.Item == "session:"+skipped || strings.HasPrefix(file.Item, "cache:") {
			t.Fatal("excluded file in snapshot")
		}
	}
	put(t, filepath.Join(cache, "new-file"), "changed excluded data")
	put(t, filepath.Join(sessionData, "sessions/new.jsonl"), "new excluded conversation")
	// Drain events produced by the test's own writes before checking resume.
	_ = readPayload()
	j, err = ResumeStage(t.Context(), p, &fakeSource{}, &progress)
	if err != nil {
		t.Fatal(err)
	}
	if readPayload() {
		t.Fatal("resume scanned an excluded payload")
	}
	if j.Phase != "prepared" {
		t.Fatal(j.Phase)
	}
	requireText(t, progress.String(), "Snapshotting selected data", "Copying selected data", "Verifying selected source snapshot", "MiB read")
	if strings.Contains(progress.String(), "Scanning cache:") {
		t.Fatal("progress includes excluded cache")
	}
	absent(t, filepath.Join(p.Work, "staged-home/cache"))
}

func TestSelectedCachesStillRequireSafeCompleteSnapshot(t *testing.T) {
	p, _ := fixture(t)
	cache := filepath.Join(p.Source, "cache/harnesses/pi/npm-cache")
	put(t, filepath.Join(cache, "package"), "cache-private")
	readPayload := watchPayloadReads(t, cache)
	v := inventory(t, p)
	if readPayload() {
		t.Fatal("discovery scanned cache")
	}
	j, err := Stage(t.Context(), v, Selection{Caches: true}, &fakeSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !readPayload() {
		t.Fatal("selected cache was not inspected")
	}
	item := j.Inventory.item("cache:pi/npm-cache")
	if !item.Scanned || item.Bytes != int64(len("cache-private")) {
		t.Fatal(item)
	}
	if read(t, filepath.Join(p.Work, "staged-home/cache/harnesses/pi/npm-cache/package")) != "cache-private" {
		t.Fatal("cache omitted")
	}
}

type callbackProgress struct {
	bytes.Buffer
	callback func(string)
}

func (w *callbackProgress) Write(b []byte) (int, error) {
	if w.callback != nil {
		w.callback(string(b))
	}
	return w.Buffer.Write(b)
}

func TestSelectedSnapshotRejectsChangesAfterScan(t *testing.T) {
	for _, change := range []string{"content", "new entry", "mode"} {
		t.Run(change, func(t *testing.T) {
			p, name := fixture(t)
			path := filepath.Join(p.Source, "sessions", name, "pi/sessions/history.jsonl")
			changed := false
			progress := &callbackProgress{callback: func(text string) {
				if changed || !strings.Contains(text, "Copying selected data...") {
					return
				}
				changed = true
				switch change {
				case "content":
					put(t, path, "changed after snapshot")
				case "new entry":
					put(t, filepath.Join(filepath.Dir(path), "new.jsonl"), "new after snapshot")
				case "mode":
					if err := os.Chmod(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}}
			j, err := Stage(t.Context(), inventory(t, p), Selection{}, &fakeSource{}, progress)
			if !changed || err == nil || j == nil || j.Phase != "staging" {
				t.Fatalf("change accepted: %v %v", j, err)
			}
			if _, err = ResumeStage(t.Context(), p, &fakeSource{}, nil); err == nil {
				t.Fatal("resume accepted changed snapshot")
			}
		})
	}
}

func TestReviewedMetadataChangesStillRefuseBeforeStaging(t *testing.T) {
	for _, change := range []string{"config", "owner"} {
		t.Run(change, func(t *testing.T) {
			p, _ := fixture(t)
			v := inventory(t, p)
			if change == "config" {
				put(t, filepath.Join(p.Source, "profiles/work/config.json"), `{"version":1,"harness":"pi"}`)
			} else {
				put(t, filepath.Join(p.Source, "profiles/new/config.json"), `{"version":1,"harness":"pi"}`)
			}
			if _, err := Stage(t.Context(), v, Selection{}, &fakeSource{}, nil); err == nil {
				t.Fatal("reviewed metadata change accepted")
			}
			absent(t, p.Work)
		})
	}
}

func TestSnapshotCancellationDoesNotCreateWorkDirectory(t *testing.T) {
	p, _ := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	progress := &callbackProgress{callback: func(text string) {
		if strings.Contains(text, "Snapshotting selected data...") {
			cancel()
		}
	}}
	if _, err := Stage(ctx, inventory(t, p), Selection{}, &fakeSource{}, progress); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	absent(t, p.Work)
}

func TestLargeReadsEmitProgressWithinAFileWithoutValues(t *testing.T) {
	var out bytes.Buffer
	progress := newPreparationProgress(&out)
	progress.begin("Snapshotting selected data")
	progress.file("public/path")
	progress.last = progress.last.Add(-2 * time.Second)
	data := []byte("private-content")
	reader := progress.reader(t.Context(), bytes.NewReader(data))
	buf := make([]byte, len(data))
	if _, err := reader.Read(buf); err != nil {
		t.Fatal(err)
	}
	requireText(t, out.String(), "1 files", "MiB read", "public/path")
	if strings.Contains(out.String(), string(data)) {
		t.Fatal("progress leaked data")
	}
	if progress.bytes != int64(len(data)) {
		t.Fatal(progress.bytes)
	}
}
