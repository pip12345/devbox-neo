package artifact

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestEntrySourcesFollowAppendOrderIncludingDuplicates(t *testing.T) {
	base := testSource(t, "base", `{"harness":"pi","harness_args":["same","same"],"mounts":["same"],"env":["DUP=base","EXPR=${env:VALUE}"],"vscode":{"extensions":["same"]},"shell":["bash","-l"]}`)
	overlay := testSource(t, "overlay", `{"harness":"pi","harness_args":["same"],"mounts":["same"],"env":["DUP=overlay"],"vscode":{"extensions":["same"]},"shell":["sh"]}`)
	last := testSource(t, "last", `{"harness":"pi","harness_args":["same"],"env":["DUP=last"]}`)
	r, err := Resolve([]config.Source{base, overlay, last}, config.Host{"VALUE": "expanded-private-value"})
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string][]string{
		"harness_args":      {"base", "base", "overlay", "last"},
		"mounts":            {"base", "overlay"},
		"env":               {"base", "base", "overlay", "last"},
		"vscode.extensions": {"base", "overlay"},
		"shell":             {"overlay"},
	} {
		if !reflect.DeepEqual(r.Trace.EntrySources[key], want) {
			t.Fatalf("%s sources = %v, want %v", key, r.Trace.EntrySources[key], want)
		}
	}
	if len(r.Trace.EntrySources["env"]) != len(r.Settings.Env) || len(r.Trace.EntrySources["harness_args"]) != len(r.Settings.HarnessArgs) {
		t.Fatal("entry metadata is not aligned with resolved values")
	}
	data, _ := json.Marshal(r.Trace)
	if strings.Contains(string(data), "expanded-private-value") || strings.Contains(string(data), "DUP=") {
		t.Fatal("provenance leaked env values")
	}
}

func TestOmittedSettingsUseOnlyBuiltinDefaults(t *testing.T) {
	source := testSource(t, "overlay", `{"env":["VALUE=value"]}`)
	r, err := Resolve([]config.Source{source}, config.Host{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Settings.Harness != "" || len(r.Trace.Sources["harness"]) != 0 || !reflect.DeepEqual(r.Trace.EntrySources["shell"], []string{"built-in default"}) {
		t.Fatal("invented harness or inherited source", r)
	}
}
