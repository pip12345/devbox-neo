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
	flags.StringVar(&f.network, "network", "", "Docker network to use: default, host, or an existing network name")
	flags.StringVar(&f.onExit, "on-exit", "", "After the last command exits: stop (stop container) or running (leave running)")
	flags.StringVar(&f.harness, "harness", "", "Choose the harness to install")
	flags.BoolVar(&f.readOnly, "read-only", false, "Mount the project folder read-only")
	flags.StringArrayVarP(&f.env, "env", "e", nil, "Set a container environment variable: KEY=VALUE (repeatable)")
	flags.StringArrayVarP(&f.mounts, "volume", "v", nil, "Mount SOURCE:TARGET[:OPTIONS] in the container (repeatable)")
	flags.StringArrayVar(&f.ports, "port", nil, "Publish [HOST_IP:]HOST_PORT:CONTAINER_PORT (repeatable)")
	flags.StringArrayVar(&f.raw, "docker-arg", nil, "Pass a Docker option, e.g. --docker-arg=--memory=2g (repeatable)")
	flags.StringArrayVar(&f.harnessArgs, "harness-arg", nil, "Pass an argument to the harness (repeatable)")
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
