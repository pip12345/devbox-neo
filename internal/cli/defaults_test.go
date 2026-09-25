package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"devbox/internal/store"
)

func TestDefaultPickerKeepsFolderRowsInPlace(t *testing.T) {
	for _, styled := range []bool{false, true} {
		name := "plain"
		if styled {
			name = "styled"
		}
		t.Run(name, func(t *testing.T) {
			enableTerminalColors(t)
			e, q, firstName := namedCLIFixture(t)
			ctx := context.Background()
			first, err := e.Store.Read(ctx, firstName)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetDefault(ctx, first); err != nil {
				t.Fatal(err)
			}
			q.LocalName = "Second"
			second, err := e.Create(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			changes := 0
			render := func(out io.Writer) error {
				input := &workflowInput{lines: []string{"3\n", "0\n", "3\n", "2\n", "0\n"}}
				input.before = func(step int) {
					if step == 2 {
						selected, err := e.Store.ReadDefault(ctx, q.Workspace)
						if err != nil || selected == nil || selected.Name != firstName || changes != 0 {
							t.Fatal("Back changed the default", selected, err)
						}
					}
				}
				return folderEditMenu(testMenu(ctx, input, out), e, q.Workspace, func(store.Record) error {
					t.Error("default selection opened the session editor")
					return nil
				}, func(string) { changes++ })
			}
			var text string
			if styled {
				text = terminalOutput(t, 80, func(out *os.File) error { return render(out) })
			} else {
				var out bytes.Buffer
				if err := render(&out); err != nil {
					t.Fatal(err)
				}
				text = out.String()
			}
			frames := strings.Split(text, menuChoicePrompt)
			if len(frames) != 6 {
				t.Fatal("unexpected frame count", text)
			}
			prefix := func(frame string) string {
				lines := strings.Split(strings.ReplaceAll(frame, "Select the folder default", "Select a session to edit"), "\n")
				// Compare everything above and including the sessions except the
				// instruction. This includes wrapping, blank lines and ANSI styles.
				for i, line := range lines {
					if strings.Contains(unstyle(line), "[2]") {
						return strings.Join(lines[:i+1], "\n")
					}
				}
				t.Fatal("missing second session row", frame)
				return ""
			}
			for _, i := range []int{1, 2, 3} {
				if prefix(frames[i]) != prefix(frames[0]) {
					t.Fatalf("session layout jumped or changed style:\n%s\n%s", frames[0], frames[i])
				}
			}
			for _, i := range []int{1, 3} {
				if !strings.Contains(frames[i], "Select the folder default") || !strings.Contains(frames[i], "[0]  Back") || strings.Contains(frames[i], "Set folder default") || strings.Contains(frames[i], "Current selection:") {
					t.Fatal("picker mixed controls or presentations", frames[i])
				}
			}
			final := strings.ReplaceAll(strings.ReplaceAll(unstyle(frames[4]), "Default: Second", "Default: Main"), "[2]  * Second", "[2]    Second")
			final = strings.ReplaceAll(final, "[1]    Main", "[1]  * Main")
			if prefix(final) != prefix(unstyle(frames[0])) {
				t.Fatal("saving the default shifted session rows", frames[4])
			}
			selected, err := e.Store.ReadDefault(ctx, q.Workspace)
			if err != nil || selected == nil || selected.Name != second.Name || changes != 1 {
				t.Fatal("default selection did not save exactly once", selected, changes, err)
			}
		})
	}
}
