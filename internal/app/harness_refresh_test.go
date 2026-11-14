package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"devbox/internal/commanderror"
	"devbox/internal/environment"
	"devbox/internal/store"
)

func harnessRefreshArgument(args []string) string {
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, environment.HarnessRefreshArg+"="); ok {
			return value
		}
	}
	return ""
}

func TestHarnessRefreshOnlyInvalidatesInstallationCache(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, image := range []bool{false, true} {
			t.Run(fmt.Sprintf("custom=%t/image=%t", custom, image), func(t *testing.T) {
				e, d, q := fixture(t)
				if custom {
					for _, source := range q.Sources {
						write(t, filepath.Join(source.Path, "docker/Dockerfile"), "ARG DEVBOX_BASE\nFROM ${DEVBOX_BASE}\n")
					}
				}
				ctx := context.Background()
				made, err := e.Create(ctx, q)
				if err != nil {
					t.Fatal(err)
				}
				before := sessionRecord(t, e, made.SessionID)
				start := len(d.History())
				request := recreateRequest(q)
				request.Options.RefreshHarness = true
				result, err := e.Recreate(ctx, request, image)
				if err != nil {
					t.Fatal(err)
				}
				var builds [][]string
				for _, args := range d.History()[start:] {
					if args[0] == "build" {
						builds = append(builds, args)
					}
				}
				want := 2
				if custom {
					want += 2*len(q.Sources) - 1
				}
				if len(builds) != want {
					t.Fatal("refresh reused the old image or skipped a captured stage", builds)
				}
				for i, args := range builds {
					if slices.Contains(args, "--no-cache") != image {
						t.Fatal("wrong stage cache policy", i, args)
					}
					if slices.Contains(args, "--provenance=false") != (i < len(builds)-1) {
						t.Fatal("only intermediate builds must disable attestations", i, args)
					}
					if (harnessRefreshArgument(args) != "") != (!image && i == len(builds)-1) {
						t.Fatal("refresh token must affect only the final stage, unless --image takes precedence", i, args)
					}
				}
				after := sessionRecord(t, e, made.SessionID)
				if before.ID != after.ID || before.Applied.ContainerID == after.Applied.ContainerID || before.Applied.ImageID == after.Applied.ImageID || !reflect.DeepEqual(before.Applied.Inputs, after.Applied.Inputs) {
					t.Fatal("refresh did not replace only the runtime association", before, after)
				}
				if len(result.Diagnostics) != 1 || !image && !strings.Contains(result.Diagnostics[0].Message, "Earlier image stages may use cache.") {
					t.Fatal("refresh did not explain its cache policy", result.Diagnostics)
				}
				buildCount := count(d, "build")
				request.Options.RefreshHarness = false
				request.Options.ForceContainer = true
				if _, err := e.Recreate(ctx, request, false); err != nil {
					t.Fatal(err)
				}
				if count(d, "build") != buildCount || sessionRecord(t, e, made.SessionID).Applied.ImageID != after.Applied.ImageID {
					t.Fatal("ordinary replacement did not retain the refreshed image")
				}
			})
		}
	}
}

func TestHarnessRefreshTokensAreInvocationScoped(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprintf("all=%t", all), func(t *testing.T) {
			e, d, q := fixture(t)
			ctx := context.Background()
			for _, name := range []string{"one", "two"} {
				q.LocalName = name
				if _, err := e.Create(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			for _, args := range d.History() {
				if harnessRefreshArgument(args) != "" {
					t.Fatal("ordinary creation received a refresh token", args)
				}
			}
			previous := ""
			for range 2 {
				start := len(d.History())
				if all {
					if _, err := e.RecreateAll(ctx, false, RecreateOptions{RefreshHarness: true}); err != nil {
						t.Fatal(err)
					}
				} else {
					request := recreateRequest(q)
					request.Options.RefreshHarness = true
					if _, err := e.Recreate(ctx, request, false); err != nil {
						t.Fatal(err)
					}
				}
				token, installations := "", 0
				for _, args := range d.History()[start:] {
					if args[0] != "build" {
						continue
					}
					if slices.Contains(args, "--no-cache") {
						t.Fatal("refresh disabled cache reuse", args)
					}
					if value := harnessRefreshArgument(args); value != "" {
						if token != "" && value != token {
							t.Fatal("sessions in one invocation received different tokens")
						}
						token = value
						installations++
					}
				}
				want := 1
				if all {
					want = 2
				}
				if installations != want || token == "" || token == previous {
					t.Fatal("each invocation must share one fresh token", installations, token, previous)
				}
				previous = token
			}
		})
	}
}

func TestHarnessRefreshBuildFailureKeepsOldRuntime(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Start(ctx, made.Session, ""); err != nil {
		t.Fatal(err)
	}
	before := sessionRecord(t, e, made.SessionID)
	failure := errors.New("harness installation failed")
	d.Fail = func(args []string) error {
		if args[0] == "build" && harnessRefreshArgument(args) != "" {
			return failure
		}
		return nil
	}
	request := recreateRequest(q)
	request.Options.RefreshHarness = true
	if _, err := e.Recreate(ctx, request, false); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if after := sessionRecord(t, e, made.SessionID); !reflect.DeepEqual(before, after) {
		t.Fatal("failed installation advanced the record")
	}
	if c, exists := sessionSnapshot(t, e, made.SessionID); !exists || c.ID != before.Applied.ContainerID || !c.State.Running {
		t.Fatal("failed installation changed the old runtime", c, exists)
	}
}

func TestHarnessRefreshAllSharesTokenAfterBuildFailure(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	for _, name := range []string{"pi", "opencode"} {
		q.LocalName = name
		q.Sources[0].Path = filepath.Join(e.Store.Home, "profiles", name)
		write(t, filepath.Join(q.Sources[0].Path, "config.json"), fmt.Sprintf(`{"version":1,"harness":%q}`, name))
		if _, err := e.Create(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	failure := errors.New("first installation failed")
	var tokens []string
	d.Fail = func(args []string) error {
		if args[0] == "build" {
			if token := harnessRefreshArgument(args); token != "" {
				tokens = append(tokens, token)
				if len(tokens) == 1 {
					return failure
				}
			}
		}
		return nil
	}
	report, err := e.RecreateAll(ctx, false, RecreateOptions{RefreshHarness: true})
	if !errors.Is(err, failure) || len(report.Results) != 2 || report.Results[0].Status != BatchFailed || report.Results[1].Status != BatchSucceeded {
		t.Fatal("bulk refresh did not continue after an installation failure", report, err)
	}
	if len(tokens) != 2 || tokens[0] == "" || tokens[0] != tokens[1] {
		t.Fatal("different image plans must keep the invocation's token after a failure", tokens)
	}
}

func TestHarnessRefreshRequiresIdleSession(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	made, err := e.Create(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := e.Store.Lock(ctx, made.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lock.Lease("test")
	lock.Close()
	if err != nil {
		t.Fatal(err)
	}
	before := runtimeMutations(d)
	request := recreateRequest(q)
	request.Options.RefreshHarness = true
	_, singleErr := e.Recreate(ctx, request, false)
	_, bulkErr := e.RecreateAll(ctx, false, request.Options)
	for _, err := range []error{singleErr, bulkErr} {
		var busy *commanderror.Error
		if !errors.As(err, &busy) || busy.Code != "session_busy" {
			t.Fatal("harness refresh bypassed idle checks", err)
		}
	}
	if runtimeMutations(d) != before {
		t.Fatal("busy harness refresh mutated runtime")
	}
}

func TestHarnessRefreshAllPreservesHistoryAndLifetime(t *testing.T) {
	e, d, q := fixture(t)
	ctx := context.Background()
	var records []store.Record
	for _, name := range []string{"stopped", "running", "missing"} {
		q.LocalName = name
		made, err := e.Create(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if name == "running" {
			if _, err := e.Start(ctx, made.Session, ""); err != nil {
				t.Fatal(err)
			}
		}
		r := sessionRecord(t, e, made.SessionID)
		write(t, filepath.Join(e.Store.Home, "sessions", r.Directory, r.StateTree, "harnesses/pi/stores/home/history"), name)
		if name == "missing" {
			forgetSession(t, e, r.ID)
		}
		records = append(records, r)
	}
	builds := count(d, "build")
	applied, err := e.RecreateAll(ctx, false, RecreateOptions{RefreshHarness: true})
	if err != nil || len(applied.Results) != len(records) || count(d, "build") != builds+2*len(records) {
		t.Fatal("bulk refresh did not build for all selected sessions", applied, err)
	}
	for _, before := range records {
		after := sessionRecord(t, e, before.ID)
		c, exists := sessionSnapshot(t, e, before.ID)
		if !exists || c.ID == before.Applied.ContainerID || c.State.Running != before.Settings.ManualStart || !reflect.DeepEqual(before.Settings, after.Settings) || !before.Created.Equal(after.Created) {
			t.Fatal("bulk refresh lost identity or lifetime", before.Settings.LocalName, c, after)
		}
		if string(getFile(t, filepath.Join(e.Store.Home, "sessions", before.Directory, before.StateTree, "harnesses/pi/stores/home/history"))) != before.Settings.LocalName {
			t.Fatal("bulk refresh changed saved history")
		}
	}
}
