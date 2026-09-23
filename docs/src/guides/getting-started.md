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

A **harness** is the coding tool Devbox launches, such as Pi or OpenCode. A **config directory** supplies reusable settings and optional customization files.

Create a config named `base`:

```sh
devbox-neo config create base
```

Choose your harness, then choose **Continue** to skip optional files for now. You can add them later through `config edit base`. This config lives in `~/.devbox-neo/configs/base/`; creating it does not create a session.

## 3. Create an environment

The command is `devbox-neo create <folder>`: replace `<folder>` with your project's path. `.` means “the current folder,” so you can go to your project first:

```sh
cd /path/to/your/project
```

Then create its environment:

```sh
devbox-neo create .
```

Enter a session name in the blank prompt, select `base` from the config picker, then choose **Create session**. The name belongs to this workspace; other workspaces can use the same name.

You can also use a path directly, such as `devbox-neo create /path/to/your/project`, without changing folders.

Devbox builds the image, installs the selected harness, and prepares the container. The first build can take a while. When creation finishes, the environment is ready but stopped.

Your project folder appears at `/workspace` inside the container. Changes made there are changes to your real project files.

## 4. Select a default and open it

Choose the session to use when you address this folder without a name:

```sh
devbox-neo edit .
```

Choose **Set folder default**, then select the session you just created. Even a folder with only one session needs an explicit default. Then open it:

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
