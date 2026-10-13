# Devbox Neo

Devbox Neo gives your project a Docker environment for Pi, OpenCode, or Claude Code. Your agent can install development tools inside the container without installing them on your host.

Devbox handles the Docker setup and saves environments as named **sessions**. Return to a session later, or rebuild its container while keeping saved conversation history. Your project folder is mounted directly: **edits inside the container change your real files**.

Use the terminal menu or direct commands. Devbox is a convenience tool, **not a security sandbox**. Give the agent only the files, credentials, and network access you trust it to use.

## Get started

Follow [Getting started](guides/getting-started.md) to build Devbox, choose a coding tool, and open your first session on a Linux host.

## Choose your next task

- [Everyday use](guides/everyday-use.md) — resume work, use a shell, and select a default.
- [Configs](guides/configuration.md) — reuse and change settings.
- [Customization](guides/customization.md) — install tools and add scripts.
- [Networking](guides/networking.md) — reach your dev server and other services.
- [SSH sharing](guides/ssh.md) — let the agent use a connection you authenticate.
- [Manage environments](guides/managing-environments.md) — recreate, copy, recover, and delete.

## Look up

[Commands](reference/commands.md) · [Configuration](reference/configuration.md) · [Harnesses](reference/harnesses.md) · [Persistence](reference/state-and-sessions.md) · [Output and errors](reference/output.md)

## Architecture

[Architecture overview](architecture/runtime.md).
