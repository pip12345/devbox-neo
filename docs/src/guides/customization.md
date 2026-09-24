# Customize your environment

Start with a [config directory](configuration.md), then add only the files you need. These examples customize the shared `base` config. For workspace-specific customization, edit a directory such as `./devconfig` and select it in the session's source chain.

## Add tools

For a quick experiment, open a shell and install a tool inside the container. That installation survives stop/start, but not recreation.

For tools you want every time, open the config editor:

```sh
devbox-neo config edit base
```

Choose **Add optional files**, toggle **Dockerfile**, then **Continue**. Edit `~/.devbox-neo/configs/base/Dockerfile`, for example:

```dockerfile
ARG DEVBOX_BASE
FROM ${DEVBOX_BASE}

RUN sudo apt-get update \
    && sudo apt-get install -y --no-install-recommends ripgrep \
    && sudo rm -rf /var/lib/apt/lists/*
```

Your Dockerfile runs as the development user. Use sudo for system packages; install user tools without it. Common tools such as Bash, Git, curl, Vim, jq, zip, and unzip are already included. Devbox installs the selected harness afterward.

Keep `FROM ${DEVBOX_BASE}`. Choose another upstream image through the config's **Base image** setting; see [base-image requirements](../reference/configuration.md#image-inputs).

Apply the change to the workspace's default session:

```sh
devbox-neo recreate .
```

Use `--name NAME` to address another session. Dockerfiles build in the session's source order, each extending the previous image. Each Dockerfile's directory is its own build context. Add a `.dockerignore` to exclude unrelated files from that context.

## Configure your harness

In `config edit base`, choose **Add optional files**, then **Harness config files**. Choose which harness's files to add and continue. Existing files are kept; this choice does not change the config's Harness setting.

Edit the generated files under `~/.devbox-neo/configs/base/pi/` or `opencode/`. For example, Pi provider definitions belong in `pi/models.json`, and custom skills belong under `pi/skills/`. Use the harness's own documentation for their format.

Managed-file changes apply when the container next starts. In an automatic environment, the container stops when the last attached command exits; the next `open .` starts it and synchronizes the files. If you used `start .` to keep it running, finish attached commands, then `stop .` and `start .` to restart it. Devbox does not synchronize files when you attach to a container that is already running.

**Edit the source directory for lasting changes.** Ordinary managed files inside the container are overwritten at the next synchronization. Shared JSON preserves keys outside Devbox's ownership; unmanaged files and conversations remain untouched.

Only regular files are copied from harness config trees. If dependency symlinks were skipped, install those dependencies inside the container instead.

## Run setup once per container

Use `setup.sh` for preparation needing the mounted workspace, such as installing project dependencies. In the directory editor's **Add optional files** menu, select **setup.sh** and continue.

Edit `~/.devbox-neo/configs/base/setup.sh`. Scripts run in source order inside the container during creation and recreation. Failure stops the chain. Setup changes require recreation; keep reusable tool installation in Dockerfiles so it can be cached.

## Run a script each time you open

Add **before-open.sh** through the same optional-file menu, then edit `~/.devbox-neo/configs/base/before-open.sh`.

Scripts run in source order inside the container before each harness launch through `open`. Keep them quick. Each script is a separate process; its completed side effects are not rolled back if a later script fails.

See [configuration artifacts](../reference/configuration.md#artifacts) for exact lifecycle rules and [harness definitions](../reference/harnesses.md) to integrate another harness.
