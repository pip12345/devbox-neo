# Devbox Neo

Run Pi, OpenCode, or Claude Code in a persistent Docker environment for your project. Keep development tools off your host, share settings across projects, and rebuild containers without losing saved harness history.

Devbox is a convenience tool, **not a security sandbox**. Give it only the files, credentials, and network access you trust your agent to use.

## Start here

You need Linux, Docker, and Go 1.24.2+ to build from source.

```sh
make build
export PATH="$PWD/bin:$PATH"
cd /path/to/your/project
devbox-neo
```

Choose **Create session**. Set a name, then **Create config** to select your coding tool. Finish creation and choose **Open**.

[Getting started](docs/src/guides/getting-started.md) walks through those steps. Then learn [everyday use](docs/src/guides/everyday-use.md), [customization](docs/src/guides/customization.md), or [SSH sharing](docs/src/guides/ssh.md).

For lookup: [commands](docs/src/reference/commands.md) · [configuration](docs/src/reference/configuration.md) · [all docs](docs/src/index.md).
