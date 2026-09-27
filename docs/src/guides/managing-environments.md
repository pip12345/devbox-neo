# Manage environments

The container is replaceable. Saved session data holds its identity and harness history; your project files stay in their host folder.

## Apply configuration changes

Choose **Status** in the session menu after changing configuration.

| Status | Next step |
|---|---|
| No changes | Keep working |
| Runtime changes | Follow the listed reasons: managed files need a container restart; launch options apply at the next Open |
| Recreate needed | Choose Recreate |
| Rebuild + recreate needed | Choose Recreate; it also builds the changed image |
| Cannot check | Fix the reported problem first |

**Recreate loses container-local files and tools**, but preserves saved harness state and project files. Put repeatable tool installation in a [Dockerfile](customization.md#add-tools).

Use **Rebuild image without cache** only when you want to rerun the build. It does not guarantee a newer upstream base image.

## Restore a missing container

A session marked **missing** may still have saved data. Try **Open** to restore its recorded container. If required inputs are unavailable, follow the error or choose **Recreate** to use current configuration.

## Copy or move a session

Choose **Copy or move** in the session menu. Set the destination folder and name, preview the result, then transfer.

- **Copy** keeps the source and leaves the destination stopped. Stop the source first.
- **Move** removes the source after the destination is ready and preserves its running intent.
- Finish attached commands before either operation.

Transfers copy saved harness state—not project files, config directories, installed container-local tools, or authentication. Prepare destination files/configs separately. [Reference rules](configuration.md#choose-where-configs-live) determine which config paths the destination uses.

You can also copy under a new name in the same folder. From the host:

```sh
devbox-neo copy . --name work --as experiment
```

The destination name must be unused. Neither copy nor move selects a destination default.

If interrupted, fix the reported problem and retry the same transfer. Leave pending state in place so Devbox can resume.

Choose **Rename session** to change its label without rebuilding or moving history. To point the same session at a moved project, choose **Change workspace**, then **Recreate**. The workspace edit clears a matching old-folder default; it does not move project files. See [CLI syntax](../reference/commands.md).

## Delete

Choose **Delete** in the session menu:

- **Container only** keeps saved data/history so you can restore the environment later.
- **Container and saved data/history** removes the saved session too.

Review **Preview deletion**, then choose **Delete…**. Whole-session deletion asks separately about the container and saved data. **Declining the second question does not restore a container already removed.**

Neither choice deletes project files, configs, managed authentication, or shared caches. **Force** permits interrupting active container commands; it does not bypass saved-data protections.

For bulk cleanup, use **All-session operations → Bulk deletion** and review the selected targets. [Deletion flags](../reference/commands.md#deletion) cover scripting and filters.
