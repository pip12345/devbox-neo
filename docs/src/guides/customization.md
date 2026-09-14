# Customize your environment

Start with a [profile or project](configuration.md), then add only the files you need. The examples below customize the `basic` profile. For project-specific files, use `project init .` and edit the corresponding files under `.devbox/`.

## Add tools

For a quick experiment, open a shell and install a tool inside the container. That installation survives stop/start, but not recreation.

For tools you want every time, open the profile setup menu:

```sh
devbox-neo profile init basic
```

Choose **Dockerfile** from the optional files. Then edit `~/.devbox-neo/profiles/basic/Dockerfile`. For example:

```dockerfile
FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ripgrep \
    && rm -rf /var/lib/apt/lists/*
```

Use a Debian-compatible base. Devbox adds its runtime tools and your chosen harness on top. Common tools such as Bash, Git, curl, sudo, Vim, jq, zip, and unzip are already included.

Apply the image change to an existing profile environment:

```sh
devbox-neo recreate . --profile basic
```

The Dockerfile's directory is its build context, so `COPY` can use sibling files. Add a `.dockerignore` to keep unrelated files out of the build.

## Configure your harness

Open the profile setup menu again:

```sh
devbox-neo profile init basic
```

Choose **harness-config** from the optional files. Edit the generated files under `~/.devbox-neo/profiles/basic/pi/` or `opencode/`, depending on your harness. Re-running init keeps your harness choice and existing files.

For example, Pi provider definitions belong in `pi/models.json`, and custom skills belong under `pi/skills/`. Use the harness's own documentation for their format.

To apply file changes to an existing container, close active commands and restart it:

```sh
devbox-neo stop . --profile basic
devbox-neo start . --profile basic
```

Devbox synchronizes managed files before starting a stopped container. It leaves them alone while the container is running.

**Edit the profile or project copy for lasting changes.** Edits to ordinary managed files inside the container are overwritten at the next synchronization. Pi's shared JSON files preserve settings outside the keys Devbox manages. Unmanaged files and conversations are left alone.

Only regular files are copied from harness configuration trees. If Devbox warns that dependency symlinks were skipped, install those dependencies inside the container instead.

## Run setup once per container

Use `setup.sh` for preparation that needs the mounted workspace, such as installing project dependencies. Open the setup menu:

```sh
devbox-neo profile init basic
```

Choose **setup.sh** from the optional files, then edit `~/.devbox-neo/profiles/basic/setup.sh`. It runs inside the container during creation and recreation. Changes to it need `recreate`.

## Run a script each time you open

Use `entrypoint.sh` for work that should happen before each harness launch. Open the setup menu:

```sh
devbox-neo profile init basic
```

Choose **entrypoint.sh** from the optional files, then edit `~/.devbox-neo/profiles/basic/entrypoint.sh`. It runs inside the container on each `open`, not on the host. Keep it quick so opening your environment stays quick.

See [configuration artifacts](../reference/configuration.md#artifacts) for exact lifecycle rules and [harness definitions](../reference/harnesses.md) if you need to integrate a different harness.
