package harness

import (
	"reflect"
	"testing"
)

func TestMountParentsIdentifyTheBackingFilesystem(t *testing.T) {
	d := Definition{Version: 1, Name: "custom", Binary: "tool", Config: Config{Store: "state", Path: "."}, Stores: []Store{
		{Name: "state", Scope: "environment", Target: "/home/devuser/.custom/state"},
		{Name: "cache", Scope: "cache", Target: "/home/devuser/.cache/custom"},
	}, Auth: []Auth{
		{Source: "tokens", Kind: "directory", Target: "/home/devuser/.custom/state/private/tokens"},
		{Source: "file.json", Kind: "file", Target: "/home/devuser/.custom/state/private/tokens/nested/file.json"},
		{Source: "cache.json", Kind: "file", Target: "/home/devuser/.cache/custom/credentials/file.json"},
	}}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	parents := d.MountParents()
	for i, parent := range parents {
		if i > 0 && parents[i-1].Target >= parent.Target {
			t.Fatal("parents are not sorted and unique")
		}
		got[parent.Target] = parent.Mount
	}
	want := map[string]string{
		"/home/devuser":                                     "",
		"/home/devuser/.cache":                              "",
		"/home/devuser/.cache/custom":                       "/home/devuser/.cache/custom",
		"/home/devuser/.cache/custom/credentials":           "/home/devuser/.cache/custom",
		"/home/devuser/.custom":                             "",
		"/home/devuser/.custom/state":                       "/home/devuser/.custom/state",
		"/home/devuser/.custom/state/private":               "/home/devuser/.custom/state",
		"/home/devuser/.custom/state/private/tokens":        "/home/devuser/.custom/state/private/tokens",
		"/home/devuser/.custom/state/private/tokens/nested": "/home/devuser/.custom/state/private/tokens",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parents: %#v", got)
	}
}
