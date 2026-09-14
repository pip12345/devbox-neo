# Getting started

This guide takes you from a checkout of Devbox to running Pi or OpenCode in your project.

You need Linux, Docker, and a non-root account that can run Docker commands. Run the commands below on the host, not inside a Devbox container.

## 1. Build the CLI

From the Devbox repository:

```sh
make build
```

For Bash, add this to `~/.bashrc`, replacing `/path/to/devbox` with your checkout's location:

```bash
# Devbox
export PATH="/path/to/devbox/bin:$PATH"
alias dbx="devbox-neo"
source <(devbox-neo completion bash)
```

This makes Devbox available from any folder, adds the `dbx` shortcut, and enables tab completion.

Reload your shell configuration and check the CLI:

```bash
source ~/.bashrc
devbox-neo version
```

## 2. Choose your harness

A **harness** is the coding tool Devbox launches, such as Pi or OpenCode. A **profile** is a reusable set of environment settings, including your harness choice.

Create a profile named `basic` and open its setup menu:

```sh
devbox-neo profile create basic
devbox-neo profile init basic
```

Choose your harness when prompted. Press Enter to skip the optional files for now; you can add them later.

Then select `basic` in the default-profile menu:

```sh
devbox-neo profile set
```

## 3. Create an environment

The command is `devbox-neo create <folder>`: replace `<folder>` with your project's path. `.` means “the current folder,” so you can go to your project first:

```sh
cd /path/to/your/project
```

Then create its environment:

```sh
devbox-neo create .
```

You can also use a path directly, such as `devbox-neo create /path/to/your/project`, without changing folders.

Devbox builds the image, installs the selected harness, and prepares the container. The first build can take a while. When creation finishes, the environment is ready but stopped.

Your project folder appears at `/workspace` inside the container. Changes made there are changes to your real project files.

## 4. Open it

```sh
devbox-neo open .
```

Devbox starts the container and launches your harness. Follow its login or provider setup when prompted. Devbox keeps its managed authentication on the host so you can reuse it later.

When the last attached Devbox command exits, the container stops by default. Your project files and saved harness state remain.

## 5. Come back later

From the same project folder:

```sh
devbox-neo open . --continue
```

`--continue` asks the harness to resume its previous conversation. You can also use the shorthand `-c`:

```sh
devbox-neo open . -c
```

Leave the flag out for a normal launch. You only need `create` once for each environment.

Use `devbox-neo list` to find your environments, or `devbox-neo shell .` to open a shell instead of the harness.

**Next:** [Everyday use](everyday-use.md).
