package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"devbox/internal/resource"
	"github.com/spf13/cobra"
)

func TestScalarEditPersistsBeforeTheNextPrompt(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	n := fieldNumber(t, "profile", "network")
	out, err := runMenu(t, s, owner, n+"\n1\nhost\n")
	if err != nil {
		t.Fatal(out, err)
	}
	source, _ := s.ConfigSource(owner)
	if string(source["network"]) != `"host"` {
		t.Fatal("completed input was not immediately saved", source)
	}
	for _, text := range []string{"[1] Yes", "[2] No", "Save changes", "Discard changes", "unconfirmed"} {
		if strings.Contains(out, text) {
			t.Fatal("scalar edit retained an approval step", out)
		}
	}
}

// The hook simulates another config writer after the menu reads its source but
// before the user submits an operation. It needs no timing-dependent goroutine.
type menuReadHook struct {
	io.Reader
	before func()
}

func (r *menuReadHook) Read(p []byte) (int, error) {
	if r.before != nil {
		before := r.before
		r.before = nil
		before()
	}
	return r.Reader.Read(p)
}

func TestImmediateListConflictReloadsWithoutOverwritingOrRetrying(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	if err := s.SetConfigField(context.Background(), owner, "harness", nil, json.RawMessage(`"pi"`), false); err != nil {
		t.Fatal(err)
	}
	key := "harness_args"
	n := fieldNumber(t, "profile", key)
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(io.MultiReader(
		strings.NewReader(n+"\n1\n"),
		&menuReadHook{
			Reader: strings.NewReader("--conflicting\n1\n--next\n0\n0\n"),
			before: func() {
				if err := s.SetConfigField(context.Background(), owner, key, nil, json.RawMessage(`["--external"]`), false); err != nil {
					t.Fatal(err)
				}
			},
		},
	))
	if err := runConfigMenu(cmd, s, owner); err != nil {
		t.Fatal(out.String(), err)
	}
	source, _ := s.ConfigSource(owner)
	entries, err := configEntries(source[key], resource.ConfigField{Kind: "list"})
	if err != nil || strings.Join(entries, ",") != "--external,--next" {
		t.Fatal("conflict overwrote another edit or retried the rejected add", entries, err)
	}
	if !strings.Contains(out.String(), "Not saved:") || !strings.Contains(out.String(), "setting changed") {
		t.Fatal("conflict was hidden", out.String())
	}
}

func TestImmediateListValidationPreservesTheLastSavedValue(t *testing.T) {
	for _, tt := range []struct {
		key, initial, input string
	}{
		{"ports", `["8080:80"]`, "1\n99999:80\n0\n0\n"},
		{"shell", `["bash"]`, "3\n1\n0\n0\n"},
	} {
		t.Run(tt.key, func(t *testing.T) {
			s := menuService(t)
			owner, _ := s.Profile("basic")
			s.Create(context.Background(), owner, "")
			if err := s.SetConfigField(context.Background(), owner, tt.key, nil, json.RawMessage(tt.initial), false); err != nil {
				t.Fatal(err)
			}
			before, _ := s.ConfigSource(owner)
			out, err := runMenu(t, s, owner, fieldNumber(t, "profile", tt.key)+"\n"+tt.input)
			if err != nil || !strings.Contains(out, "Not saved:") {
				t.Fatal(out, err)
			}
			after, _ := s.ConfigSource(owner)
			if !bytes.Equal(before[tt.key], after[tt.key]) {
				t.Fatal("invalid operation changed the saved field")
			}
		})
	}
}

func TestExtensionListOperationsUseTheSameImmediateModel(t *testing.T) {
	s := menuService(t)
	owner, _ := s.Profile("basic")
	s.Create(context.Background(), owner, "")
	n := fieldNumber(t, "profile", "vscode")
	out, err := runMenu(t, s, owner, n+"\n1\nexample.one\n2\n1\nexample.two\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	source, _ := s.ConfigSource(owner)
	entries, err := configEntries(source["vscode"], resource.ConfigField{Kind: "extensions"})
	if err != nil || strings.Join(entries, ",") != "example.two" {
		t.Fatal("extension add/edit did not persist", entries, err)
	}
	out, err = runMenu(t, s, owner, n+"\n4\n0\n0\n")
	if err != nil {
		t.Fatal(out, err)
	}
	source, _ = s.ConfigSource(owner)
	if _, exists := source["vscode"]; exists {
		t.Fatal("extension reset retained the source field")
	}
}
