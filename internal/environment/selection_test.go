package environment

import (
	"strings"
	"testing"
)

func TestLocalNamesAreExactAndBounded(t *testing.T) {
	workspace := t.TempDir()
	for _, name := range []string{"Main", "main", "1", "a-b_c", strings.Repeat("a", 64)} {
		id, err := Identify(workspace, name)
		if err != nil || id.LocalName != name || id.Validate() != nil || !strings.HasSuffix(id.Name, "."+name) {
			t.Fatal(id, err)
		}
	}
	for _, name := range []string{"", ".", "..", "a.b", "-first", "_first", "two words", "a/b", "é", strings.Repeat("a", 65)} {
		if _, err := Identify(workspace, name); err == nil {
			t.Fatal("accepted invalid local name", name)
		}
	}
	if ContainerName(workspace, "Main") == ContainerName(workspace, "main") {
		t.Fatal("case-distinct local names share identity")
	}
	if got := ContainerName("/work/api", "Main"); got != "devbox-api-21d4e96b0656.Main" {
		t.Fatal(got)
	}
	if got := ContainerName("/work/api", "main"); got != "devbox-api-239c7814b3f4.main" {
		t.Fatal(got)
	}
	if got := ContainerName("/"+strings.Repeat("x", 80), strings.Repeat("N", 64)); len(got) != 117 {
		t.Fatal("full name length changed", len(got))
	}
}
