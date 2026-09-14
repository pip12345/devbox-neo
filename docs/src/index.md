# Devbox

Devbox runs coding harnesses such as Pi and OpenCode alongside your development tools in a Docker container, with your project folder mounted inside. You can leave and return to the same environment, reuse settings across projects, and rebuild the container without losing saved conversation history.

## Learn to use Devbox

Start with the basics, then add what your work needs:

1. [Getting started](guides/getting-started.md) — create an environment and launch your harness.
2. [Everyday use](guides/everyday-use.md) — return to work, open a shell, and run commands.
3. [Profiles and project settings](guides/configuration.md) — share defaults and customize individual projects.
4. [Customize your environment](guides/customization.md) — add tools, harness configuration, and startup scripts.
5. [Networking](guides/networking.md) — expose a development server and connect to other services.
6. [Share SSH access](guides/ssh.md) — authenticate once and let your agent use the connection.
7. [Manage environments](guides/managing-environments.md) — apply changes, copy saved state, and clean up.

## Look something up

- [Commands](reference/commands.md)
- [Configuration](reference/configuration.md)
- [Harness definitions](reference/harnesses.md)
- [State and sessions](reference/state-and-sessions.md)
- [Output and errors](reference/output.md)

## Understand the implementation

[Architecture](architecture/runtime.md) covers the component layout, lifecycle, configuration pipeline, persistent state, and SSH connection management.
