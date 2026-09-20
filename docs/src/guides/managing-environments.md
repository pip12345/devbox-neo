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
| Runtime changes | Restart a running container to synchronize managed files |
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

Use `status --all` to review every saved environment. Opening an environment with changed image or container settings warns and keeps using the recorded settings until you recreate it.

## Recover a missing container

An environment can still appear in `list` after its container has been removed. If its recorded image and other required inputs are available, restore it with:

```sh
devbox-neo start .
```

`open` can restore it too. If recovery reports unavailable inputs, use `recreate` to build from current configuration. Set environment variables in profile/project configuration, using host references for secrets.

## Copy saved harness state to another folder

Prepare the destination workspace and its profile or project settings first. Transfers copy saved harness state, not your project files or tools installed only inside the container.

Close attached commands, then stop and copy:

```sh
devbox-neo stop /path/to/project
devbox-neo copy /path/to/project /path/to/copy --dry-run
devbox-neo copy /path/to/project /path/to/copy
```

The destination must not already have an environment in the selected slot. Copy leaves it stopped and keeps the source.

To move rather than copy, add `--move` to `copy`. It removes the source environment after the destination is ready and preserves whether it was running.

You can also move between profile and project environments in the same folder:

```sh
devbox-neo copy . --move --from .profile-basic --to .profile-basic.project
```

Here, a **slot** means the selected profile/project combination. The destination project must already be initialized. See [transfer commands](../reference/commands.md#transfers) for selector rules.

If a transfer is interrupted, inspect `list` or `status <exact-name>`, fix the reported problem, and retry the same command. Leave pending state in place so Devbox can resume safely.

## Delete an environment

For an interactive choice:

```sh
devbox-neo delete .
```

Devbox first asks about removing the container, then separately asks about saved data and conversation history. Keep the saved data if you want to restore the environment later.

For an explicit, non-prompting action:

```sh
devbox-neo delete . --container
```

That keeps saved state. Use `--session` instead to delete the whole environment, including its saved history. Neither scope deletes workspace files, profile/project configuration, managed auth, or shared caches.

To preview cleanup of saved environments whose containers are missing and whose last Devbox activity was more than 30 days ago:

```sh
devbox-neo delete --session --orphaned --older-than 720h --dry-run
```

Review the selection, then remove `--dry-run` to delete it. See [deletion flags](../reference/commands.md#deletion) for other filters and active-command protections.
