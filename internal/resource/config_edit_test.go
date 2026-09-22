package resource

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"devbox/internal/config"
)

func TestConfigEditsPreserveSourceAndConcurrentFields(t *testing.T) {
	s := fixture(t)
	o, _ := s.ConfigDirectory("basic", t.TempDir(), t.TempDir())
	path := filepath.Join(o.Root, "config.json")
	put(t, path, `{"version":1,"harness":"${env:NOT_SET}","env":["TOKEN=${env:NOT_SET}"],"network":"default"}`)
	source, err := s.ConfigSource(o)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = s.SetConfigField(ctx, o, "network", source["network"], json.RawMessage(`"host"`), false); err != nil {
		t.Fatal(err)
	}
	if err = s.SetConfigField(ctx, o, "shell", source["shell"], json.RawMessage(`["sh"]`), false); err != nil {
		t.Fatal("unrelated concurrent edit was blocked", err)
	}
	if err = s.SetConfigField(ctx, o, "network", source["network"], json.RawMessage(`"default"`), false); !errors.Is(err, ErrConfigChanged) {
		t.Fatal("stale same-field edit was not rejected", err)
	}
	current, _ := s.ConfigSource(o)
	if string(current["network"]) != `"host"` || !sameConfigValue(current["shell"], json.RawMessage(`["sh"]`)) || string(current["harness"]) != string(source["harness"]) || !sameConfigValue(current["env"], source["env"]) {
		t.Fatal("edit flattened expressions or lost unrelated changes", current)
	}
	if err = s.SetConfigField(ctx, o, "env", source["env"], json.RawMessage(`["TOKEN=${env:NOT_SET}","SECOND=value"]`), false); err != nil {
		t.Fatal("formatting changes caused a false conflict", err)
	}
	if err = s.SetConfigField(ctx, o, "network", current["network"], nil, true); err != nil {
		t.Fatal(err)
	}
	current, _ = s.ConfigSource(o)
	if _, exists := current["network"]; exists {
		t.Fatal("remove materialized a default value instead of removing the key")
	}
	if err = s.SetConfigField(ctx, o, "network", nil, json.RawMessage(`"${env:FUTURE_NETWORK}"`), false); err != nil {
		t.Fatal("source edit resolved an expression prematurely", err)
	}
}

func TestConfigEditingReferenceFormsAndValidation(t *testing.T) {
	for _, target := range []string{"basic", filepath.Join(t.TempDir(), "local")} {
		t.Run(target, func(t *testing.T) {
			s := fixture(t)
			o, err := s.ConfigDirectory(target, t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CreateConfig(context.Background(), o, SetupOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetConfigField(context.Background(), o, "harness", nil, json.RawMessage(`"pi"`), false); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(o.Root, "config.json")
			before := string(get(t, path))
			for _, test := range []struct{ key, value string }{
				{"harness", `123`}, {"harness", `null`}, {"harness", `"missing"`}, {"version", `2`}, {"not_a_field", `true`},
				{"inherit", `false`}, {"default_harness", `"pi"`}, {"on_exit", `"invalid"`}, {"network", `"not a network"`},
				{"shell", `[]`}, {"ports", `["99999:80"]`}, {"env", `["DEVBOX_BAD=secret-value"]`},
			} {
				current, _ := s.ConfigSource(o)
				if err := s.SetConfigField(context.Background(), o, test.key, current[test.key], json.RawMessage(test.value), false); err == nil || strings.Contains(err.Error(), "secret-value") {
					t.Fatalf("accepted invalid %s or exposed a secret: %v", test.key, err)
				}
				if string(get(t, path)) != before {
					t.Fatal("invalid edit changed source")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			current, _ := s.ConfigSource(o)
			if err := s.SetConfigField(ctx, o, "harness", current["harness"], nil, true); !errors.Is(err, context.Canceled) {
				t.Fatal("canceled edit not rejected", err)
			}
			if string(get(t, path)) != before {
				t.Fatal("canceled edit changed source")
			}
		})
	}
}

func TestConfigFieldControlsCoverSchema(t *testing.T) {
	typ := reflect.TypeOf(config.Layer{})
	want := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if key != "-" && key != "version" {
			want[key] = true
		}
	}
	for _, field := range ConfigFields() {
		if !want[field.Key] {
			t.Fatalf("unknown or duplicate control: %s", field.Key)
		}
		delete(want, field.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing controls: %v", want)
	}
}
