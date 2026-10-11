package store

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/environment"
)

func TestTransferRetryCommandsKeepExactMetadataEndpoints(t *testing.T) {
	workspace, destination := t.TempDir(), t.TempDir()
	source := environment.Binding{Workspace: workspace, LocalName: "Main"}
	to := environment.Binding{Workspace: destination, LocalName: "Main"}
	other := environment.Binding{Workspace: workspace, LocalName: "Experiment"}
	id := strings.Repeat("a", 12)
	for _, test := range []struct {
		mode     string
		from, to environment.Binding
	}{
		{"relocate", source, to}, {"clone", source, to}, {"relocate", source, other}, {"clone", other, source},
	} {
		journal := Transfer{Mode: test.mode, Source: test.from, Destination: test.to, SourceID: id}
		want := []string{"dbx", "copy"}
		if test.mode == "relocate" {
			want = append(want, "--move")
		}
		want = append(want, environment.PublicName(test.from, id), test.to.Workspace, "--as", test.to.LocalName)
		if got := journal.RetryStep(); !reflect.DeepEqual(got.Command, want) {
			t.Fatal(got.Command, want)
		}
	}
	j := Transfer{Source: source, Destination: to, SourceID: id}
	before := j.RetryStep()
	if err := os.Remove(workspace); err != nil {
		t.Fatal(err)
	}
	if got := j.RetryStep(); !reflect.DeepEqual(got, before) {
		t.Fatal("missing workspace retargeted retry")
	}
}
