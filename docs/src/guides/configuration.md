# Choose configs for a session

A config directory holds settings and optional customization files. Several sessions can share one. Each session uses the configs you explicitly select, in order.

## Change shared settings

Run `devbox-neo config`, select `base`, and press **Enter**. Or go straight to it:

```sh
devbox-neo config edit base
```

Choose a setting to edit it. Changes save immediately; leaving the editor does not undo them. **Remove this setting** removes the value from this config, allowing an earlier config or the built-in default to supply it.

Editing `base` affects every session using it. The editor lists those sessions.

## Add settings for one project

From your project folder, create a separate config:

```sh
devbox-neo config create ./devconfig
```

Leave **Harness** unset when `base` already selects your coding tool. Choose **Create config**, then edit its settings:

```sh
devbox-neo config edit ./devconfig
```

The directory only takes effect after you select it in a session.

## Select the configs to use

Open the session's menu in `devbox-neo`, then **Edit selected configs**. Choose **Add existing config** and enter `./devconfig`.

Keep `base` first and `./devconfig` second. Later scalar settings replace earlier ones; most lists append. For example, the project config can change the shared network setting without choosing another harness.

Use the same menu to replace, remove, or reorder configs. **Show combined configuration** shows the final values and where they came from. See the [field table](../reference/configuration.md#config-fields) for exact merge rules.

## Choose where configs live

- `base` refers to `~/.devbox-neo/configs/base/`. It stays at that fixed path when a session is copied or moved.
- `./devconfig` is selected relative to your current directory and follows the session's workspace during a transfer.

Config directories themselves are not copied. [Reference rules](../reference/configuration.md#locations-and-references) cover other path forms.

## Apply your changes

Saving settings does not replace a container. Choose **Status** in the session menu to see what needs applying.

Managed harness files apply when the container next starts. Container settings and build changes need **Recreate**. Recreation preserves saved harness state but loses files and tools stored only inside the container.

If you no longer need a named config, use **Delete config** in its editor. Devbox refuses while sessions still use it; follow the listed sessions to remove the dependency first.

**Next:** [Customize your environment](customization.md).
