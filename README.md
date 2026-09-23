# Devbox Neo

Devbox Neo is a convenience tool for running coding agents in persistent Docker workspaces. You get the useful parts of a containerized workflow: tools off your host, repeatable setups, and state that survives between containers. Unlike security-focused sandboxes such as Docker Sandbox, Devbox doesn't try to police a trusted agent. It keeps things simple: no proxy to configure or permission system to manage. Set up the environment and let the agent work.

Pi and OpenCode are built in. Use reusable configs, Dockerfiles, and scripts to build the environment you want, or add another harness. The agent can install whatever tools it needs inside the container, including system packages with `sudo`.

You can also [share an SSH connection](docs/src/guides/ssh.md) with the agent to work on another server or device without handing over any credentials.

## Philosophy

Devbox is not a security sandbox. There is no managed proxy, egress policy, or agent permission gate. The agent can use the files, credentials, and network access you give it. That's deliberate.

The point is less setup and more convenience. You stay responsible for watching what it does.

[Getting started](docs/src/guides/getting-started.md) · [Customization](docs/src/guides/customization.md) · [Commands](docs/src/reference/commands.md)

## Quick start

Linux and Docker required. From this checkout:

```sh
make build
export PATH="$PWD/bin:$PATH"
cd /path/to/your/project
devbox-neo config create base  # choose Pi or OpenCode
devbox-neo create .            # pick a name and select base
devbox-neo edit .              # set the folder default
devbox-neo open .
```