package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devbox/internal/docker"
)

type warningWriter func([]byte) (int, error)

func (w warningWriter) Write(p []byte) (int, error) { return w(p) }

func TestCreationWarningPrecedesStartupWithoutDelay(t *testing.T) {
	for _, mode := range []string{"container", "image", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			write(t, filepath.Join(e.Store.Home, "profiles/test/entrypoint.sh"), "echo entrypoint-marker")
			result, err := createAndOpen(ctx, e, q)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "image" {
				write(t, filepath.Join(e.Store.Home, "profiles/test/Dockerfile"), "FROM debian:bookworm-slim\n")
				write(t, filepath.Join(e.Store.Home, "profiles/test/.dockerignore"), "pi/\n")
			} else {
				write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
			}
			if mode == "recovery" {
				d.Forget(result.Name)
			}
			write(t, filepath.Join(e.Store.Home, "profiles/test/pi/new-file"), "new config")
			if err := os.Symlink("new-file", filepath.Join(e.Store.Home, "profiles/test/pi/skipped-link")); err != nil {
				t.Fatal(err)
			}
			liveFile := filepath.Join(e.Store.Home, "sessions", result.Name, "harnesses/pi/stores/home/new-file")
			var output bytes.Buffer
			warned := false
			before := len(d.History())
			writer := warningWriter(func(p []byte) (int, error) {
				if strings.Contains(string(p), "this container differs from current configuration") {
					if output.Len() != 0 || len(d.History()) != before {
						t.Fatal("creation warning was not first", output.String())
					}
					if _, err := os.Stat(liveFile); !os.IsNotExist(err) {
						t.Fatal("config synchronized before warning", err)
					}
					warned = true
				}
				return output.Write(p)
			})
			e.Streams.Out, e.Streams.Err = writer, writer
			d.Fail = func([]string) error {
				if !warned {
					return errors.New("Docker reached before warning")
				}
				return nil
			}
			d.Attached = func(_ context.Context, c docker.Command) error {
				if c.Stdin != nil {
					data, err := io.ReadAll(c.Stdin)
					if err != nil {
						return err
					}
					if strings.Contains(string(data), "entrypoint-marker") {
						_, err = io.WriteString(c.Stdout, "entrypoint output\n")
						return err
					}
				}
				return nil
			}
			openCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
			result, err = e.Open(openCtx, q)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "creation_drift" {
				t.Fatal("typed warning missing", result)
			}
			if strings.Count(output.String(), "this container differs from current configuration") != 1 || !strings.Contains(output.String(), "skipping non-regular") || !strings.Contains(output.String(), "entrypoint output") {
				t.Fatal("warning duplicated or subsequent output lost", output.String())
			}
			if string(getFile(t, liveFile)) != "new config" {
				t.Fatal("open did not continue with synchronization")
			}
		})
	}
}

func TestOpenCancelledAfterCreationWarningDoesNotMutate(t *testing.T) {
	e, d, q := fixture(t)
	result, err := createAndOpen(context.Background(), e, q)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","network":"host"}`)
	p, _ := e.Store.RecordPath(result.Name)
	beforeRecord := string(getFile(t, p))
	beforeCalls := len(d.History())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.Streams.Err = warningWriter(func(p []byte) (int, error) {
		if strings.Contains(string(p), "this container differs from current configuration") {
			cancel()
		}
		return len(p), nil
	})
	result, err = e.Open(ctx, q)
	if !errors.Is(err, context.Canceled) || len(result.Diagnostics) != 1 {
		t.Fatal("open ignored cancellation", result, err)
	}
	if len(d.History()) != beforeCalls || string(getFile(t, p)) != beforeRecord {
		t.Fatal("cancelled open changed Docker or the session record")
	}
	lockCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	lock, err := e.Store.Lock(lockCtx, result.Name)
	if err != nil {
		t.Fatal("cancelled open leaked operation lock", err)
	}
	lock.Close()
}

func TestOpenWithoutCreationDriftDiagnostics(t *testing.T) {
	e, _, q := fixture(t)
	write(t, filepath.Join(e.Store.Home, "profiles/test/config.json"), `{"version":1,"harness":"pi","on_exit":"running"}`)
	if _, err := createAndOpen(context.Background(), e, q); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"unchanged", "runtime-only", "runtime-deferred"} {
		switch change {
		case "runtime-only":
			write(t, filepath.Join(e.Store.Home, "profiles/test/entrypoint.sh"), "echo runtime-only")
		case "runtime-deferred":
			write(t, filepath.Join(e.Store.Home, "profiles/test/pi/new-file"), "deferred config")
		}
		result, err := e.Open(context.Background(), q)
		if err != nil {
			t.Fatal(change, err)
		}
		if change == "runtime-deferred" {
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "runtime_deferred" {
				t.Fatal("deferred config warning missing", result)
			}
		} else if len(result.Diagnostics) != 0 {
			t.Fatal("unexpected drift warning", result)
		}
	}
}
