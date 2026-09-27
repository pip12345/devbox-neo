package environment

import (
	"strings"
	"testing"
)

func TestLocalNamesAreExactAndBounded(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"Main", "main", "1", "a-b_c", strings.Repeat("a", 64)} {
		selection, err := Identify(workspace, name)
		if err != nil || selection.LocalName != name || selection.Binding.Validate() != nil {
			t.Fatal(selection, err)
		}
	}
	for _, name := range []string{"", ".", "..", "a.b", "-first", "_first", "two words", "a/b", "é", strings.Repeat("a", 65)} {
		if _, err := Identify(workspace, name); err == nil {
			t.Fatal("accepted invalid name", name)
		}
	}
}

func TestSessionTargetsAreIDsNotStorageOrContainerNames(t *testing.T) {
	if !IsSessionTarget(strings.Repeat("a", 32)) {
		t.Fatal("rejected session ID")
	}
	for _, target := range []string{"dbx-api-abc.work", "work", "/work/api", strings.Repeat("a", 31)} {
		if IsSessionTarget(target) {
			t.Fatal("accepted non-ID target", target)
		}
	}
}
