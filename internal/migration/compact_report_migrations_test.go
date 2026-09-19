package migration

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestCompactReportPreservesWarningsAndScopesSharedNotes(t *testing.T) {
	v := &Inventory{Paths: Paths{Source: "/old", Destination: "/neo", Work: "/stage"}, Notices: []string{"Originals stay untouched.", "Not imported: /old/usage"}}
	v.Items = append(v.Items, Item{Key: "auth:opencode", Kind: "auth", Path: "/external/auth.json", Notices: []string{"External authentication needs approval."}})
	for n := 0; n < 20; n++ {
		i := Item{Key: fmt.Sprintf("session:example-%02d", n), Kind: "session", Path: "/old/sessions/example", Harness: "pi", Workspace: fmt.Sprintf("/work/%d", n), Profile: "work", SessionID: "full-session-id", Target: "full-target", Created: "full-created", Activity: "full-activity", Alias: "old-alias", RelocatedFrom: []string{"old-workspace"}, Notices: []string{"Rebuild runs setup hooks.", "Compare creation settings before merge."}}
		if n < 2 {
			i.Changes = []string{"Proxy settings removed."}
		}
		if n == 0 {
			i.Issues = []string{"Workspace missing; restore it or explicitly skip."}
			i.Warnings = []string{"Not imported: retained claude at /old/harnesses/claude."}
		}
		if n == 1 {
			i.Warnings = []string{"Not imported: retained codex at /old/harnesses/codex."}
		}
		v.Items = append(v.Items, i)
	}
	var full, compact bytes.Buffer
	if err := Report(&full, v, nil); err != nil {
		t.Fatal(err)
	}
	if err := CompactReport(&compact, v, nil); err != nil {
		t.Fatal(err)
	}
	text := compact.String()
	requireText(t, text, "Items: 21 | Error: 1 | Warning: 1 | Metadata OK: 19", "Authentication", "Sessions", "Path: /external/auth.json", "External authentication needs approval.", "Shared notes summary:", "1. Review: Rebuild runs setup hooks.", "3. Conversion: Proxy settings removed.", "--verbose", "No data has been copied")
	for _, i := range v.Items {
		requireText(t, text, i.Key)
		for _, msg := range append(append([]string{}, i.Issues...), i.Warnings...) {
			requireText(t, text, msg)
		}
	}
	for note, count := range map[string]int{"1. Review: Rebuild runs setup hooks.": 21, "2. Review: Compare creation settings before merge.": 21, "3. Conversion: Proxy settings removed.": 3} {
		if strings.Count(text, note) != count {
			t.Fatal("note must appear beside each affected item and in the summary", note)
		}
	}
	if strings.Contains(text, "[notes ") {
		t.Fatal("item requires a note lookup")
	}
	for _, metadata := range []string{"full-session-id", "full-target", "full-created", "full-activity", "old-alias", "old-workspace"} {
		requireText(t, full.String(), metadata)
		if strings.Contains(text, metadata) {
			t.Fatal("compact output includes full metadata", metadata)
		}
	}
	for _, notice := range v.Notices {
		requireText(t, text, notice)
	}
	if strings.Contains(text, "Portable data: not scanned") {
		t.Fatal("repeated payload disclaimer")
	}
	if strings.Count(text, "\n") >= strings.Count(full.String(), "\n")/2 {
		t.Fatal("compact report did not substantially reduce output")
	}
}

func TestInlineNotesUseGlobalNumbersInAscendingOrder(t *testing.T) {
	items := []Item{
		{Key: "profile:a", Kind: "profile", Notices: []string{"First.", "Second."}, Changes: []string{"Third."}},
		{Key: "profile:b", Kind: "profile", Notices: []string{"Second.", "First."}, Changes: []string{"Third."}},
		{Key: "profile:c", Kind: "profile", Notices: []string{"Second."}},
	}
	notes := collectReportNotes(items)
	for n, item := range items {
		var b strings.Builder
		compactItem(&b, item, nil, "Metadata OK", notes, reportPaint{})
		if n < 2 {
			requireText(t, b.String(), "    1. Review: First.\n    2. Review: Second.\n    3. Conversion: Third.\n")
		} else {
			requireText(t, b.String(), "    2. Review: Second.\n")
			if strings.Contains(b.String(), "1.") || strings.Contains(b.String(), "3.") {
				t.Fatal("notes were renumbered or shown on an unaffected item")
			}
		}
	}
	var b bytes.Buffer
	if err := CompactReport(&b, &Inventory{Items: items}, nil); err != nil {
		t.Fatal(err)
	}
	requireText(t, b.String(), "Shared notes summary:\n  1. Review: First.\n  2. Review: Second.\n  3. Conversion: Third.\n")
}

func TestCompactReportStatesAndExclusions(t *testing.T) {
	i := Item{Key: "session:example", Kind: "session", Scanned: true}
	for _, tc := range []struct {
		name, want string
		j          *Journal
	}{
		{"scanned", "Scanned", nil},
		{"staged", "Staged", &Journal{Phase: "prepared"}},
		{"failed", "Failed", &Journal{Failure: "interrupted", CurrentItem: i.Key}},
		{"skipped", "Skipped", &Journal{Excluded: map[string]string{i.Key: "user excluded"}}},
		{"pending", "Pending", &Journal{Merge: &MergeState{}}},
		{"imported", "Imported", &Journal{Merge: &MergeState{Attempts: map[string]*Attempt{i.Key: {Phase: "done"}}}}},
		{"merge excluded", "Skipped", &Journal{Merge: &MergeState{Plan: MergePlan{Excluded: map[string]string{i.Key: "merge excluded"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportItemState(i, tc.j); got != tc.want {
				t.Fatal(got)
			}
			var b strings.Builder
			compactItem(&b, i, tc.j, reportItemState(i, tc.j), collectReportNotes([]Item{i}), reportPaint{})
			requireText(t, b.String(), "["+tc.want+"] "+i.Key, "Portable bytes:")
			if tc.name == "merge excluded" {
				requireText(t, b.String(), "Exclusion: merge excluded")
			}
		})
	}
}

func TestCompactReportEscapesUntrustedText(t *testing.T) {
	v := &Inventory{Items: []Item{{Key: "profile:bad\n[OK] injected", Kind: "profile", Warnings: []string{"unsafe\x1b[2Jwarning"}}}}
	var b bytes.Buffer
	if err := CompactReport(&b, v, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "bad\n[OK]") || strings.Contains(b.String(), "\x1b") {
		t.Fatal("unsafe terminal output")
	}
	requireText(t, b.String(), `bad\n[OK] injected`, `unsafe\x1b[2Jwarning`)
}
