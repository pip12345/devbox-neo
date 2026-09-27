# Everyday use

Run `devbox-neo` on the host. Highlight a session and press **Enter** for its actions.

## Resume work

Choose **Continue** to resume a conversation, or **Open** for a normal launch. The session list shows **Last active** times. These include recorded operations such as Stop and Exec, not just harness launches.

The harness takes over your terminal. When it exits, acknowledge the result to return. **Ctrl-C** interrupts a foreground operation. In a menu, it exits Devbox. See [keyboard controls](../reference/output.md#interactive-menus) for navigation.

## Open a shell

Choose **Shell** to work inside the container yourself. It starts in `/workspace`. Type `exit` to return.

Tools installed here survive stop/start, but are lost when the container is recreated. Use a [Dockerfile](customization.md#add-tools) for repeatable installation.

## Select a folder default

A project can have several named sessions. In a session's menu, choose **Make folder default** to select the one folder-only commands should use. The list marks it with `*`. The same menu then offers **Clear folder default**.

Selecting a default does not launch anything. Opening a session from the browser does not require a default.

## Use command shortcuts

From your project folder, `.` means “this folder.” These commands use its selected default:

```sh
devbox-neo open . --continue
```

To open a shell instead:

```sh
devbox-neo shell .
```

Or run one command and return:

```sh
devbox-neo exec . -- git status
```

The `--` separates Devbox options from the command. Add `--name work` before it to select a named session instead of the default. Use `devbox-neo list` to find sessions and `devbox-neo status .` to inspect one.

## Keep services running

Normally, the container stops when its last attached command exits. Choose **Start** to keep it running until **Stop**, even after Docker restarts. This does not resume old harness processes or terminals after a reboot.

Stop refuses while Devbox commands are active. Finish them first. Use Force only when you intend to interrupt them.

## Pass harness options

Use **Open with options**, or put one-off arguments after `--`. For Pi's regular terminal mode:

```sh
devbox-neo open . -- --tui-mode regular
```

Put options you use every time in your config's **Harness arguments** setting instead.

**Next:** [Choose configs](configuration.md).
