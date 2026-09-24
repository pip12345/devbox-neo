# Choose configs for a session

A **config directory** contains settings and optional Dockerfiles, scripts, and harness files. Several sessions can share it. Each session explicitly selects an ordered list of config directories; files are not discovered automatically in your workspace.

Named configs live under `~/.devbox-neo/configs/`. You can also keep configs anywhere on the host, including beside your project files. To see the named configs in your selected Devbox home, including their directory paths and ones that need repair, run `devbox-neo config list`. Configs stored elsewhere appear in a session's config list, not in this inventory.

## Edit a config

Open the directory editor:

```sh
devbox-neo config edit base
```

The editor shows the directory's path and every saved session using it. Choose a setting and enter its value. Each valid edit saves immediately. Use `0` or `q` to go back or exit, and `:back` to cancel text entry.

**Remove this setting** removes the key from this config. It does not write a built-in or another config's value into the file. The dashboard shows only this directory over built-in defaults; its list editors change only entries stored here.

Use **Add optional files** to add missing customization files without overwriting existing ones. Adding harness files does not force a harness selection: an overlay can supply Pi files while leaving its Harness setting unset.

To remove an unused named config and all its files, run `devbox-neo config delete base` and confirm. Devbox refuses while saved sessions still select it or need its committed files, and lists every affected session. Remove this config from those sessions and recreate them (or delete the sessions) first. This command does not remove configs at arbitrary paths outside the selected home's `configs/` directory.

## Add workspace-specific settings

Create an ordinary config directory in your workspace:

```sh
devbox-neo config create ./devconfig
```

Choose **Leave unset** if another selected config already chooses your harness, then **Continue**. Edit the directory when you need settings such as extra mounts or ports:

```sh
devbox-neo config edit ./devconfig
```

This directory does not affect a session until you select it there.

## Change a session's configs

From the workspace, open the session picker:

```sh
devbox-neo edit .
```

Select a session, choose **Add existing config**, and enter `./devconfig`. The folder menu also lets you choose **Set folder default** or **Clear folder default**. Keep `base` before it if you want the workspace config's explicit scalar values to override the shared config. Lists generally append; [the reference](../reference/configuration.md#config-fields) describes field-specific rules.

Adding, replacing, removing, or reordering selected configs saves immediately. **Exit** does not undo those changes. The editor prints an exact-session `status` command on exit so you can check whether the container needs updating. Editing a config directory prints bare `status` instead, to review changes across environments. It changes this session only, without renaming it or copying the directories. Editing a shared directory instead affects every session that uses it.

Choose **Show combined configuration** to see effective values and which configs supplied them. For non-interactive inspection, provide the local name explicitly:

```sh
devbox-neo edit . --name Main --show
```

Missing configs remain visible so you can replace or remove them. You can save an incomplete config list during repair, but opening or recreating requires at least one config and a valid combined configuration, including a harness.

## Choose portable or fixed references

Relative arguments initially resolve against the directory where you run the command. They are then stored relative to the session's workspace. For example, `./devconfig` follows the workspace when you copy or move the session.

Bare names such as `base`, absolute paths, and home-relative paths such as `~/configs/personal` are fixed references. They keep using the same absolute directory after a transfer. Devbox does not copy config directories for you.

## Pass environment variables

You can edit a directory's `config.json` directly. This example passes a token from the host without storing its value in the file:

```json
{
  "version": 1,
  "env": ["WORK_TOKEN=${env:WORK_TOKEN}"]
}
```

Set `WORK_TOKEN` in your host environment before running Devbox. An unset reference is an error. Prefer environment references for credentials: menu input is visible, and ordinary settings such as argv and paths are not secret fields.

## Apply changes

Saving config does not replace an existing container. Check its pending changes:

```sh
devbox-neo status . --name Main
```

Use the local name of the session you edited (or the exact command printed when you exit). Managed harness files synchronize when the container next starts; they do not require recreation. For a container deliberately kept running with `start`, stop it after attached commands finish and start it again. Container settings, image inputs, and setup changes require:

```sh
devbox-neo recreate . --name Main
```

Container-local files and tools are lost during recreation. See [managing environments](managing-environments.md#apply-configuration-changes) before replacing a customized container.

**Next:** [Customize your environment](customization.md).
