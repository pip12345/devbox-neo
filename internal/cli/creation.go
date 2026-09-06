package cli

import (
	"devbox/internal/app"
	"devbox/internal/config"
	"github.com/spf13/cobra"
)

type creationFlags struct {
	network, onExit, harness             string
	readOnly                             bool
	env, mounts, ports, raw, harnessArgs []string
}

func (f *creationFlags) Bind(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&f.network, "network", "", "Primary Docker network")
	flags.StringVar(&f.onExit, "on-exit", "", "After the last attached command: stop or running")
	flags.StringVar(&f.harness, "harness", "", "Select an effective harness definition")
	flags.BoolVar(&f.readOnly, "read-only", false, "Mount the workspace read-only at creation")
	flags.StringArrayVarP(&f.env, "env", "e", nil, "Container KEY=VALUE (sensitive; invocation-only values require explicit recreation for recovery)")
	flags.StringArrayVarP(&f.mounts, "volume", "v", nil, "Extra SOURCE:/absolute/target[:options] mount")
	flags.StringArrayVar(&f.ports, "port", nil, "Published [HOST_IP:]HOST_PORT:CONTAINER_PORT")
	flags.StringArrayVar(&f.raw, "docker-arg", nil, "Raw Docker option; value-taking options use --option=value")
	flags.StringArrayVar(&f.harnessArgs, "harness-arg", nil, "Append an exact non-secret harness argument")
}
func (f *creationFlags) Request(cmd *cobra.Command, workspace, profile string) app.Request {
	q := app.Request{Workspace: workspace, Profile: profile, ReadOnly: f.readOnly}
	q.Overrides = config.Layer{Env: f.env, Mounts: f.mounts, Ports: f.ports, DockerArgs: f.raw, HarnessArgs: f.harnessArgs}
	if cmd.Flags().Changed("network") {
		q.Overrides.Network = &f.network
	}
	if cmd.Flags().Changed("on-exit") {
		q.Overrides.OnExit = &f.onExit
	}
	if cmd.Flags().Changed("harness") {
		q.Overrides.Harness = &f.harness
	}
	return q
}
