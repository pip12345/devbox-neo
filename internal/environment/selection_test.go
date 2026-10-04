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

func TestSessionTargetsAreBareDirectoryNames(t *testing.T) {
	if !IsSessionTarget("dbx-api-abc.work") {
		t.Fatal("rejected session directory name")
	}
	for _, target := range []string{strings.Repeat("a", 32), "work", "/work/api", "./dbx-api-abc.work", "dbx-../work", "dbx-bad\nname"} {
		if IsSessionTarget(target) {
			t.Fatal("accepted non-session target", target)
		}
	}
	if !IsSessionID(strings.Repeat("a", 32)) || IsSessionID("dbx-api-abc.work") {
		t.Fatal("internal ID validation changed")
	}
}
