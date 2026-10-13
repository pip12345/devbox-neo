# Manage environments

The container is replaceable. Saved session data holds its identity and harness history. Your project files stay in their host folder.

## Apply configuration changes

Choose **Status** in the session menu after changing configuration. Open/Continue keep using the existing settings until you choose **Recreate**.

| Status | Next step |
|---|---|
| No changes | Keep working |
| Runtime changes | Choose Recreate to apply without replacing the container |
| Recreate needed | Choose Recreate |
| Rebuild + recreate needed | Choose Recreate to build the changed image and replace the container |
| Cannot check | Fix the reported error before applying changes |

Finish attached commands first. Recreate may briefly start or restart the container. **Container replacement loses local files and tools**, but preserves harness history and project files. Put repeatable tool installation in a [Dockerfile](customization.md#add-tools).

Use **Force container replacement** to replace an otherwise unchanged container. **Rebuild image without cache** also replaces it and reruns the build. It does not guarantee a newer upstream base image.

If you need to interrupt attached commands, choose **Force replacement even with attached commands**, or run `dbx recreate . --force` from the session's default folder. This always replaces the container and loses its local files and tools. Saved history remains. Automatic sessions stop after replacement. Explicitly started sessions keep running.

## Restore a missing container

For a session marked **missing**, choose **Recreate** to build a replacement container from its current configs. Saved harness history is kept. If recreation reports missing files or invalid configs, fix the problem and retry. Then choose **Open** or **Continue**.

## Copy or move a session

Choose **Copy or move** in the session menu. Set the destination folder and name, preview the result, then transfer.

- **Copy** keeps the source and leaves the destination stopped. Stop the source first.
- **Move** removes the source after the destination is ready and preserves its running intent.

Prepare destination files/configs separately. [Reference rules](configuration.md#choose-where-configs-live) determine which config paths the destination uses.

You can also copy under a new name in the same folder. From the host:

```sh
dbx copy . --name work --as experiment
```

The destination name must be unused. Neither copy nor move selects a destination default.

If interrupted, fix the error and retry the same command. **Abort pending transfer**, when offered, discards the incomplete copy and keeps the source. Do not delete transfer files by hand.

## Rename a session or change its project folder

Choose **Rename session** to change its name without rebuilding or moving history.

If you have moved the project yourself, choose **Change workspace** to save its new location, then **Recreate** to mount that folder. Devbox does not move project files. Changing the workspace clears the old folder's default if it selected this session. See [CLI syntax](../reference/commands.md).

## Delete

Choose **Delete** in the session menu:

- **Container only** keeps saved data/history so you can restore the environment later.
- **Container and saved data/history** removes the saved session too.

Review **Preview deletion**, then choose **Delete…**. Whole-session deletion asks separately about the container and saved data. **Declining the second question does not restore a container already removed.**

Neither choice deletes project files, configs, managed authentication, or shared caches. **Force** permits interrupting active container commands. It does not bypass saved-data protections.

For bulk cleanup, use **All-session operations → Bulk deletion** and review the selected targets. [Deletion flags](../reference/commands.md#deletion) cover scripting and filters.
