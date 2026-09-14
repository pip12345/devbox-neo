# Profiles and project settings

Use a **profile** for settings you reuse across projects. Use **project settings** for choices that belong with one codebase. Global settings choose defaults for your machine.

Devbox stores profiles under `~/.devbox-neo/profiles/` and project settings in the project's `.devbox/` directory.

## Edit your profile

Open the configuration menu:

```sh
devbox-neo profile config basic
```

Choose a setting, then enter its new value. For example, set `on_exit` to `running` if you usually want the container left running after you exit.

Each valid edit saves immediately. Use `0` to go back or exit, and `:back` to cancel text entry. Resetting a setting removes your local choice so its inherited value applies again.

To inspect settings without editing:

```sh
devbox-neo profile config basic --show
```

The source beside each value tells you where it came from. Lists can include inherited entries; the editor changes only entries in the profile you're editing.

## Add project-specific settings

From your project folder, create its configuration:

```sh
devbox-neo project create .
```

Open the setup menu:

```sh
devbox-neo project init .
```

Choose **inherit** to use the harness from your profile or global defaults, or select Pi or OpenCode for this project. Press Enter to skip optional files for now.

Then open the project settings menu:

```sh
devbox-neo project config .
```

Normally, project settings build on your default profile. Project scalar values replace profile values; lists such as mounts and harness arguments append to them. See the [field table](../reference/configuration.md#profile-and-project-fields) for exact merge rules.

Project configuration selects a separate environment for that folder. If you've only created a profile environment so far, create the project environment before opening it:

```sh
devbox-neo create .
devbox-neo open .
```

The existing profile environment remains available with `--profile basic`. An explicit `--profile` uses that profile alone and ignores project settings.

## Make a project self-contained

To stop inheriting a profile, set the project's `inherit_profile` to `false` and choose its harness explicitly.

For a new project configuration, you can start with a one-time copy of a profile:

```sh
devbox-neo project create . --from-profile basic
```

This requires a folder without an existing `.devbox/` directory. The copied project no longer follows profile changes; global defaults still apply.

## Pass environment variables

You can edit `.devbox/config.json` directly. This example passes a token from the host without storing it in the file:

```json
{
  "version": 1,
  "extra_env": ["WORK_TOKEN=${env:WORK_TOKEN}"]
}
```

Set `WORK_TOKEN` in your host environment before running Devbox. An unset reference is an error. Use environment references for credentials: menu input is visible, and ordinary settings such as command arguments and paths are not secret fields.

## Apply your changes

Saving configuration does not replace an existing container. Check what needs applying:

```sh
devbox-neo status .
```

Harness configuration files synchronize when a stopped container starts. Container settings, such as environment variables or mounts, need `recreate`:

```sh
devbox-neo recreate .
```

See [managing environments](managing-environments.md#apply-configuration-changes) before recreating a container with tools or files you added inside it.

**Next:** [Customize your environment](customization.md). For all fields and selection rules, use the [configuration reference](../reference/configuration.md).
