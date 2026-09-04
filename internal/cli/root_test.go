package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSmoke(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}} {
		cmd := New()
		out := new(bytes.Buffer)
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out.String()) == "" {
			t.Fatal("empty output")
		}
	}
}
