# Getting started

This guide takes you from a checkout of Devbox to running Pi, OpenCode, or Claude Code in your project.

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

This makes Devbox available from any folder, adds an optional `dbx` shortcut, and enables tab completion. The program's command is `devbox-neo`.

Reload your shell configuration and check the CLI:

```bash
source ~/.bashrc
devbox-neo version
```

## 2. Create your first session

Go to your project and open Devbox:

```sh
cd /path/to/your/project
devbox-neo
```

With no sessions, **Create session** starts selected. Press **Enter**. The overview shows your current folder; **Change folder** lets you choose another. Choose **Set session name** and enter a name such as `work`. Names belong to a workspace, so other workspaces can use the same name.

Choose **Create config**. A **config directory** supplies reusable settings and optional customization files. Set **Name/location** to `base`, then choose **Harness** and select your coding tool. A harness is what Devbox launches, such as Pi, OpenCode, or Claude Code. Leave **Optional files** at **None** for now. The built-in Claude harness bypasses Claude's permission prompts; see [built-in launch settings](../reference/harnesses.md#built-in-harnesses).

Choose **Create config** to save it. Devbox returns to your session draft with `base` already added. If you have an existing config instead, use **Add existing config**. A config already created remains saved even if you cancel the session draft.

Review the folder, name, and selected configs, then choose **Create session**. Devbox builds the image, installs the selected harness, and prepares the container. The first build can take a while. Build failures return to your entered choices; follow any recovery instructions before retrying.

Your project appears at `/workspace` inside the container. Changes there are changes to your real project files.

## 3. Open it

After creation, acknowledge the result to enter the new session's menu. The session is stopped, with **Open** selected. Press **Enter** to launch the harness, then follow its login or provider setup when prompted. Devbox keeps managed authentication on the host for reuse.

You do not need a folder default to open the session from this menu. **Make folder default** is available if you want folder-only commands to select it later. Creation never sets a default or launches a harness automatically.

When the last attached Devbox command exits, the container stops by default. Your project files and saved harness state remain. Acknowledge the result to return to the menu.

## 4. Come back later

Run `devbox-neo`, highlight your session on the left, and press **Enter** to open its menu. Choose **Continue** to resume its previous conversation, **Open** for a normal launch, or **Shell** for a terminal inside the container. Use **Esc** to return to browsing and **Tab** to switch to configs.

Direct commands remain available. If you made this session the folder default, from its project folder you can run:

```sh
devbox-neo open . --continue
```

Here `.` means the current folder. You can use `-c` instead of `--continue`. Without a saved default, add `--name work` to select the session explicitly. You only need to create each session once.

**Next:** [Everyday use](everyday-use.md).
