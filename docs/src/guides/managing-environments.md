# Manage environments

An environment has two parts: a Docker container you can replace, and saved session data that keeps its identity and declared harness state. Your workspace files remain in the project folder on the host.

## Apply configuration changes

After editing configuration, check the environment:

```sh
devbox-neo status .
```

The result tells you what needs to happen:

| Result | What to do |
|---|---|
| No changes | Keep working |
| Runtime changes | Check the listed reasons. Managed files sync on container restart; launch options and before-open scripts apply at the next `open` |
| Recreate needed | Run `recreate` to apply container settings |
| Rebuild + recreate needed | Run `recreate`; it also builds the changed image |
| Cannot check | Fix the reported configuration or state problem first |

To replace the container:

```sh
devbox-neo recreate .
```

Saved harness state is preserved. **Files and tools stored only in the container are lost.** Put lasting project files in `/workspace` and repeatable tool installation in a [Dockerfile or setup script](customization.md).

Ordinary recreation reuses the image when its inputs are unchanged. To force a build without cache:

```sh
devbox-neo recreate . --image
```

This reruns installation; it does not guarantee a newer upstream base image. `status` checks local inputs, not available upstream releases.

Use bare `status` to review every saved environment. Opening an environment with changed image or container settings warns and keeps using the recorded settings until you recreate it.

## Recover a missing container

An environment can still appear in `list` after its container has been removed. If its recorded image and other required inputs are available, open it normally to restore the container:

```sh
devbox-neo open .
```

Use `start .` instead only when you want to keep the container running until `stop`, including across reboots. If recovery reports unavailable inputs, use `recreate` to build from current configuration. Set environment variables in the session's selected configs, using host references for secrets.

## Copy saved harness state to another folder

Prepare the destination workspace and make its required config directories available first. Workspace-relative config references follow the destination; fixed references keep their original absolute paths. Transfers copy saved harness state, not your project files or tools installed only inside the container.

Close attached commands, then stop and copy:

```sh
devbox-neo stop /path/to/project
devbox-neo copy /path/to/project /path/to/copy --dry-run
devbox-neo copy /path/to/project /path/to/copy
```

The destination local name must be unused. Copy preserves the name unless you supply `--as NAME`, leaves the destination stopped, and keeps the source. It does not select a destination default; use `devbox-neo edit /path/to/copy` to make a session the default before opening that folder without a name.

To move rather than copy, add `--move` to `copy`. It removes the source environment after the destination is ready and preserves whether it was running.

You can also make a separately named copy in the same folder:

```sh
devbox-neo copy . --name Main --as Experiment
```

Add `--move` to change the name through a move instead. A move clears a matching source default rather than selecting the destination for you. Config directories and workspace files are not copied. See [transfer commands](../reference/commands.md#transfers) for details.

If a transfer is interrupted, inspect `list` or `status <exact-name>`, fix the reported problem, and retry the same command. Leave pending state in place so Devbox can resume safely.

## Delete an environment

For an interactive choice:

```sh
devbox-neo delete .
```

A folder target selects its saved default; use `--name NAME` to delete a different session in that folder. Devbox shows the selected session and asks whether to remove its container now. If you answer yes, it removes the container before asking separately about saved session data and conversation history. Answering no to the second question keeps that data but does not restore the container. Keep the saved data if you want to restore the environment later.

For an explicit, non-prompting action:

```sh
devbox-neo delete . --container
```

That keeps saved state. Use `--session` instead to delete the whole environment, including its saved history. Neither scope deletes workspace files, config directories, managed auth, or shared caches. Whole-session deletion clears its matching folder default; container-only deletion leaves that selection intact.

To preview cleanup of saved environments whose containers are missing and whose last Devbox activity was more than 30 days ago:

```sh
devbox-neo delete --session --orphaned --older-than 720h --dry-run
```

Review the selection, then remove `--dry-run` to delete it. See [deletion flags](../reference/commands.md#deletion) for other filters and active-command protections.
