package cli

import (
	"context"
	"errors"
	"io"

	"devbox/internal/cliui"
	"devbox/internal/commanderror"
	"devbox/internal/docker"
	"devbox/internal/migration"
	"github.com/spf13/cobra"
)

func requireMigration(cmd *cobra.Command, home string, runtime docker.Runtime) error {
	required, err := migration.Pending(cmd.Context(), home)
	if err != nil || required == nil {
		return err
	}
	blocked := migrationBlocked(home)
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON || !interactive(cmd) {
		return blocked
	}
	return migrationPrompt(cmd, home, *required, func(ctx context.Context) error {
		return required.Apply(ctx, runtime)
	})
}

func migrationBlocked(home string) *commanderror.Error {
	return commanderror.New("migration_required", "Migration required. Devbox cannot use this home until you agree to migrate. Run dbx in a terminal to continue.", home, nil,
		commanderror.Next("Review and agree to the required migration", "--home", home))
}

// This single blocking screen is shared by every state-backed entry point.
// It only presents effects; the migration service owns checks and execution.
func migrationPrompt(cmd *cobra.Command, home string, required migration.Requirement, apply func(context.Context) error) (err error) {
	// Migration output belongs on stderr, not in redirected command/JSON data.
	out := cmd.OutOrStdout()
	cmd.SetOut(cmd.ErrOrStderr())
	defer cmd.SetOut(out)
	m := newMenu(cmd)
	defer func() { err = errors.Join(err, m.Finish()) }()
	migrated := false
	err = m.Run(func() (cliui.Screen, error) {
		return cliui.Screen{
			Title: "Migration required", Back: "Exit",
			Fields: []cliui.Field{
				{Label: "Migration", Value: required.Name},
				{Label: "Home", Value: home},
				{Label: "Changes", Value: required.Description},
				{Label: "Warning", Value: required.Warning, Warning: true},
			},
			Actions: []cliui.Action{
				{Label: "Exit", Run: func() (bool, error) { return true, nil }},
				{Label: "Migrate", Danger: true, Run: func() (bool, error) {
					if err := m.Pause(); err != nil {
						return false, err
					}
					if err := apply(m.Context); err != nil {
						return false, commanderror.New("migration_failed", "Migration failed; Devbox remains blocked: "+err.Error(), home, err)
					}
					migrated = true
					return true, nil
				}},
			},
		}, nil
	})
	if errors.Is(err, io.EOF) {
		blocked := migrationBlocked(home)
		blocked.Cause = err
		return blocked
	}
	if err != nil {
		return err
	}
	if !migrated {
		return migrationBlocked(home)
	}
	return nil
}
