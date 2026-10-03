package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestMovePreparationFailureDoesNotRequirePrunedSourceImage(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	source := sessionRecord(t, e, made.SessionID)
	write(t, filepath.Join(e.Store.Home, "sessions", source.Directory, "harnesses/pi/stores/home/history"), "source history")
	forgetSession(t, e, source.ID)
	delete(d.Images, source.Applied.ImageID)
	delete(d.Images, source.Applied.ImageTag)
	opts := TransferOptions{Mode: "relocate", Source: source.ID, Destination: t.TempDir()}
	failure := errors.New("build unavailable")
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			return failure
		}
		return nil
	}
	_, err = e.Transfer(ctx, opts)
	if !errors.Is(err, failure) || strings.Contains(err.Error(), "image missing") {
		t.Fatal("rollback required pruned source image", err)
	}
	d.Fail = nil
	result, err := e.Transfer(ctx, opts)
	if err != nil {
		t.Fatal("retry could not move saved state without source runtime", err)
	}
	r := sessionRecord(t, e, result.Destination)
	if r.ID != source.ID {
		t.Fatal("move changed session identity")
	}
	if got := string(getFile(t, filepath.Join(e.Store.Home, "sessions", r.Directory, "harnesses/pi/stores/home/history"))); got != "source history" {
		t.Fatal("source-authoritative history lost", got)
	}
}
