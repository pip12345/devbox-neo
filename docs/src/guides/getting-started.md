# Getting started

This walkthrough uses the terminal menu to create your first environment and launch a coding tool.

Three terms you'll see:

- **Session:** a saved environment for one project folder that you can return to later.
- **Config:** reusable settings a session uses, such as which coding tool to launch.
- **Harness:** the coding tool Devbox launches—Pi, OpenCode, or Claude Code.

Run these commands on your **Linux host**, not inside a Devbox container. You need Docker access from a non-root account, Bash, Make, curl, and tar. The installation step downloads Go 1.24.2 locally for the build. You do not need Go installed beforehand.

## Build and launch

From the repository checkout, in Bash:

```bash
make install-go
make build
```

Add this line to `~/.bashrc`, replacing `/path/to/devbox` with your checkout's absolute path:

```bash
export PATH="/path/to/devbox/bin:$PATH"
```

Open a new terminal, then:

```bash
cd /path/to/your/project
dbx
```

## Create a session

With no sessions yet, **Create session** is selected. Press **Enter**.

1. Check the folder. Use **Change folder** if needed.
2. Choose **Set session name** and enter `work`.
3. Choose **Create config** to make a reusable set of settings.
4. Set **Name/location** to `base`. Under **Harness**, choose Pi, OpenCode, or Claude Code.
5. Leave **Optional files** at **None**, then choose **Create config**.

Devbox adds the new config to your session draft automatically. Optionally check **Make folder default** to select this session for commands such as `dbx open .`. This replaces any existing default. Choose **Create session** to build the environment. The first build can take a while.

## Open it

Your project will be at `/workspace` inside the container. **Edits there change your real project files.** Devbox is **not a security sandbox**. Only give the agent access you trust it to use.

After the build, acknowledge the result. The new session's menu opens with **Continue** selected. Press **Enter**, then follow the harness's login or provider setup.

When you leave the harness, the container normally stops. Project files and saved harness history remain. Acknowledge the result to return to Devbox.

## Return later

Run `dbx`, highlight your session, and press **Enter**. Choose **Continue** to resume the conversation or **Open** for a normal launch. **Esc** goes back. **Tab** switches to configs while browsing.

**Next:** [Everyday use](everyday-use.md). For Tab completion of commands and session names, add the optional [shell integration](../reference/commands.md#completion).
