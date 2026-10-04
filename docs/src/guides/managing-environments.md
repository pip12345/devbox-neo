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

Use **Force container replacement** to replace an otherwise unchanged container. **Rebuild image without cache** also replaces it and reruns the build; it does not guarantee a newer upstream base image.

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

Choose **Rename session** to change its label without rebuilding or moving history. To point the same session at a moved project, choose **Change workspace**, then **Recreate**. The workspace edit clears a matching old-folder default. It does not move project files. See [CLI syntax](../reference/commands.md).

## Delete

Choose **Delete** in the session menu:

- **Container only** keeps saved data/history so you can restore the environment later.
- **Container and saved data/history** removes the saved session too.

Review **Preview deletion**, then choose **Delete…**. Whole-session deletion asks separately about the container and saved data. **Declining the second question does not restore a container already removed.**

Neither choice deletes project files, configs, managed authentication, or shared caches. **Force** permits interrupting active container commands. It does not bypass saved-data protections.

For bulk cleanup, use **All-session operations → Bulk deletion** and review the selected targets. [Deletion flags](../reference/commands.md#deletion) cover scripting and filters.
