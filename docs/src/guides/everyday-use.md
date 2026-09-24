# Everyday use

Once you've [created an environment](getting-started.md), use it from your project folder or address it by the name shown in `list`.

## Find and open an environment

List your saved environments:

```sh
devbox-neo list
```

The list includes running and stopped environments, plus saved environments whose containers are missing. Its `LIFETIME` column shows `automatic` or `until stop` separately from the current container state.

From your project folder, open its environment. `.` means the current folder:

```sh
devbox-neo open .
```

Use `status` when you want more detail:

```sh
devbox-neo status .
```

Folder commands use the default you selected through `devbox-neo edit .`. Use `--name NAME` to select another session in that folder, or a full session name from `list` to target it from anywhere. `list .` shows just this folder's sessions. Missing selections fail; opening never creates a session or chooses a default.

## Open an interactive shell

Run this from a host terminal:

```sh
devbox-neo shell .
```

This opens an interactive terminal inside the container, starting it if needed. You begin in `/workspace`, where you can run commands yourself. Type `exit` to return to your host shell.

Terminal display settings are forwarded from the host; your host prompt and dotfiles are not copied.

## Run a single command

To run a command inside the container without opening an interactive shell, use `exec` from the host:

```sh
devbox-neo exec . -- git status
```

This starts the container if needed, runs `git status` in `/workspace`, and returns to your host shell. The `--` separates Devbox's arguments from the command to run.

For a pipeline or shell expression, ask a shell to interpret it:

```sh
devbox-neo exec . -- bash -lc 'git status && git diff --stat'
```

## Keep a container running

Normally the container stops when the last attached harness, shell, command, or SSH-sharing terminal exits. To keep it running until you explicitly stop it:

```sh
devbox-neo start .
```

You can run this even while a harness is open. Later attachments do not change that choice. After a reboot, Docker restarts the container when Docker starts; it does not resume your old harness process or terminal.

When you're done:

```sh
devbox-neo stop .
```

It stays stopped across reboot. Opening it again without another manual `start` restores normal automatic shutdown.

Stop refuses to interrupt active Devbox commands unless you pass `--force`.

## Pass options to the harness

Put one-off harness arguments after `--`. For example, to launch Pi in regular terminal mode rather than its default fullscreen mode:

```sh
devbox-neo open . -- --tui-mode regular
```

For options you use every time, set `harness_args` and its matching `harness` in the same [config directory](configuration.md).

## Enable completion

For Bash:

```sh
source <(devbox-neo completion bash)
```

Add that line to your shell startup file to load completion in new terminals. Devbox also generates scripts for Zsh, Fish, and PowerShell; see the [command reference](../reference/commands.md#completion).

## Get help

Use `devbox-neo <command> --help` for command options. Inside the container, the same documentation is available at `/devbox/docs/index.md`; `/devbox/AGENTS.md` gives the agent its container guidance.

**Next:** [Choose configs for a session](configuration.md).
