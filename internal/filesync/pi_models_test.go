package filesync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"devbox/internal/artifact"
	"devbox/internal/harness"
)

func TestPiModelsUsesDeclaredProvidersMerge(t *testing.T) {
	h, err := harness.Load(t.TempDir(), "pi")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	path := filepath.Join(root, "models.json")
	manifest := filepath.Join(root, "manifest.json")
	if err = os.WriteFile(path, []byte(`{"providers":{"local":{"baseUrl":"http://old"}},"unowned":"keep"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, desired, providers string
	}{
		{"replace", `{"providers":{"custom":{"baseUrl":"http://new"}},"unowned":"ignored"}`, `{"custom":{"baseUrl":"http://new"}}`},
		{"remove", `{}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			desired := map[string]artifact.File{"models.json": {Data: []byte(tt.desired)}}
			if err := Sync(root, manifest, "home", desired, h.Definition.Merge); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var current map[string]json.RawMessage
			if err = json.Unmarshal(b, &current); err != nil {
				t.Fatal(err)
			}
			if string(current["unowned"]) != `"keep"` {
				t.Fatal("undeclared key changed")
			}
			got := ""
			if value, ok := current["providers"]; ok {
				var obj any
				if err = json.Unmarshal(value, &obj); err != nil {
					t.Fatal(err)
				}
				compact, err := json.Marshal(obj)
				if err != nil {
					t.Fatal(err)
				}
				got = string(compact)
			}
			if got != tt.providers {
				t.Fatalf("providers = %s, want %s", got, tt.providers)
			}
		})
	}
}
