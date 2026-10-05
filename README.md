# Devbox Neo

Run Pi, OpenCode, or Claude Code in a persistent Docker environment for your project. Keep development tools off your host, share settings across projects, and rebuild containers without losing saved harness history.

Your agent can install whatever development tools it needs inside the container, including system packages. It can set up its own environment as it works, without making you install those dependencies on your host.

Devbox handles the Docker setup and organizes your project environments as named sessions. Open a session to work with your agent or use a shell, then return to the same environment the next time you work. You can manage sessions through the terminal menu or directly from the command line. The menu is optional.

Devbox is a convenience tool, **not a security sandbox**. Give it only the files, credentials, and network access you trust your agent to use.

## Get started

You need a Linux host with Docker access from a non-root account. Run these commands from a checkout of this repository. `make install-go` installs the local Go toolchain needed to build:

```bash
make install-go
make build
```

Add these lines to `~/.bashrc`, replacing `/path/to/devbox` with your checkout's absolute path:

```bash
export PATH="/path/to/devbox/bin:$PATH"
source <(dbx completion bash)
```

Open a new terminal, then:

```bash
cd /path/to/your/project
dbx
```

Devbox opens a menu for your project. Choose **Create session** to set up its environment. The [getting-started guide](docs/src/guides/getting-started.md) walks you through choosing a coding tool and launching it. The first build can take a while. After setup, run `dbx` whenever you want to return to your saved session.

## Learn more

Once you're working, [everyday use](docs/src/guides/everyday-use.md) covers returning to conversations and using a shell. To shape the environment around your project, see [customization](docs/src/guides/customization.md). [SSH sharing](docs/src/guides/ssh.md) explains how to let the agent use a connection you authenticate.

[All docs](docs/src/index.md) · [Command reference](docs/src/reference/commands.md) · [Config reference](docs/src/reference/configuration.md)
