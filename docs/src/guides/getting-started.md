# Getting started

Run these commands on your **Linux host**, not inside a Devbox container. You need Docker access from a non-root account and Go 1.24.2+ for the build.

## Build and launch

From the repository checkout:

```sh
make build
export PATH="$PWD/bin:$PATH"
cd /path/to/your/project
dbx
```

If Go is missing, run `make install-go` before building. The PATH change above applies to this terminal. [Shell setup](../reference/commands.md#completion) covers future terminals.

## Create a session

A session is a saved environment for one project folder. With no sessions yet, **Create session** is selected. Press **Enter**.

1. Check the folder. Use **Change folder** if needed.
2. Choose **Set session name** and enter `work`.
3. Choose **Create config** to make a reusable set of settings.
4. Set **Name/location** to `base`. Under **Harness** (the coding tool to launch), choose Pi, OpenCode, or Claude Code.
5. Leave **Optional files** at **None**, then choose **Create config**.

Devbox adds the new config to your session draft automatically. Choose **Create session** to build the environment. The first build can take a while.

**Claude Code runs with its permission prompts bypassed.** See [harness defaults](../reference/harnesses.md#built-in-harnesses) before choosing it.

## Open it

After the build, acknowledge the result. The new session's menu opens with **Continue** selected. Press **Enter** to launch the harness, then follow its login or provider setup. Choose **Open** instead to launch without requesting continuation.

Your project is at `/workspace` inside the container. **Edits there change your real project files.**

When you leave the harness, the container normally stops. Project files and saved harness history remain. Acknowledge the result to return to Devbox.

## Return later

From your project folder, run `dbx`. The first session in that folder is selected in the browser. Press **Enter** to open its menu, then **Enter** again to **Continue** the conversation. Choose **Open** instead for a fresh launch. **Esc** goes back. **Tab** switches to configs while browsing.

**Next:** [Everyday use](everyday-use.md).
