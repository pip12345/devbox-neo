package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolutionAndStatusReturnWarningsWithoutRendering(t *testing.T) {
	e, _, q := fixture(t)
	root := filepath.Join(q.Sources[0].Path, "pi")
	write(t, filepath.Join(root, "regular"), "keep")
	if err := os.Symlink("regular", filepath.Join(root, "skipped")); err != nil {
		t.Fatal(err)
	}
	stderr := new(bytes.Buffer)
	e.Streams.Err = stderr
	spec, err := e.Resolve(resolveRequest(q))
	if err != nil || len(spec.Warnings) != 1 || stderr.Len() != 0 {
		t.Fatal("resolution mixed data with presentation", spec.Warnings, stderr.String(), err)
	}
	made, err := e.Create(context.Background(), q)
	if err != nil || stderr.Len() == 0 {
		t.Fatal("creation suppressed warnings", err)
	}
	stderr.Reset()
	details, err := e.Status(context.Background(), made.Session, "")
	if err != nil || len(details.Warnings) != 1 || stderr.Len() != 0 {
		t.Fatal("status rendered rather than returned warnings", details.Warnings, stderr.String(), err)
	}
	report, err := e.StatusAll(context.Background(), "")
	if err != nil || len(report.Sessions) != 1 || len(report.Sessions[0].Warnings) != 1 || stderr.Len() != 0 {
		t.Fatal(report, stderr.String(), err)
	}
	transfer, err := e.Transfer(context.Background(), TransferOptions{Mode: "clone", Source: made.Session, Destination: t.TempDir(), DryRun: true})
	if err != nil || len(transfer.Warnings) != 1 || stderr.Len() != 0 {
		t.Fatal("transfer preview rendered warnings", transfer.Warnings, stderr.String(), err)
	}
}
