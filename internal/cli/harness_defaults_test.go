package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestBuiltinHarnessesAreAvailableWithoutSelectingADefault(t *testing.T) {
	var out bytes.Buffer
	m := testMenu(context.Background(), strings.NewReader("0\n"), &out)
	options, proceed, err := configCreationMenu(m, t.TempDir())
	if err != nil || proceed || options.Harness != nil {
		t.Fatal("listing built-ins must not choose a harness", options, proceed, err)
	}
	for _, label := range []string{"claude", "opencode", "pi", "Current selection: Unset", "Leave unset (selected)"} {
		if !strings.Contains(out.String(), label) {
			t.Fatal("missing built-in choice or unset selection", label, out.String())
		}
	}
}
