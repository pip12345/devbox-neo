package cli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSavedSessionCanCreateAndAddConfigWithoutApplyingRuntime(t *testing.T) {
	e, _, name := namedCLIFixture(t)
	before, err := e.Store.Read(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m := testMenu(context.Background(), strings.NewReader("2\n1\nfresh\n2\n1\n4\n0\n"), &out)
	saved, err := sourceChainMenu(m, e, before, "Exit")
	if err != nil || !saved {
		t.Fatal(saved, err, out.String())
	}
	after, err := e.Store.Read(context.Background(), name)
	if err != nil || after.ID != before.ID || !reflect.DeepEqual(after.Applied, before.Applied) || len(after.Sources) != 2 || after.Sources[1].Label != "fresh" {
		t.Fatal(after, err)
	}
	if !strings.Contains(out.String(), "Saved selected configs.") {
		t.Fatal(out.String())
	}
}

func TestDuplicateConfigSelectionPreservesSessionDraft(t *testing.T) {
	e, q, _ := namedCLIFixture(t)
	var out bytes.Buffer
	p, err := newSourcePicker(testMenu(context.Background(), strings.NewReader("2\n1\n0\n"), &out), e.Store.Home, q.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	before := sessionCreationDraft{workspace: q.Workspace, name: "Work", sources: q.Sources}
	after, proceed, err := createSessionMenu(p, e, before)
	if err != nil || proceed || !reflect.DeepEqual(after, before) || !strings.Contains(out.String(), "Error:") {
		t.Fatal(after, proceed, err, out.String())
	}
}
