# Customize your environment

Open `devbox-neo config edit base` and choose **Add optional files**. Add only what you need. Existing files are kept.

These examples change the shared `base` config. Use a [project config](configuration.md#add-settings-for-one-project) when the customization belongs to one workspace.

## Add tools

For a quick experiment, install a tool from **Shell**. For an installation that survives recreation, add **Dockerfile**, choose **Continue**, and edit `~/.devbox-neo/configs/base/Dockerfile`:

```dockerfile
ARG DEVBOX_BASE
FROM ${DEVBOX_BASE}

RUN sudo apt-get update \
    && sudo apt-get install -y --no-install-recommends ripgrep \
    && sudo rm -rf /var/lib/apt/lists/*
```

Keep `FROM ${DEVBOX_BASE}` so your image extends Devbox's prepared environment. Use `sudo` for system packages and normal user permissions for user tools.

Choose **Recreate** in the session menu to apply the Dockerfile. **Container-local changes will be lost.**

For alternate base images, build contexts, and ignore files, see [image inputs](../reference/configuration.md#image-inputs).

## Configure the harness

Add **Harness config files**, choose the harness, and continue. Edit the generated directory under `~/.devbox-neo/configs/base/`: `pi/`, `opencode/`, or `claude/`.

For example, Pi's provider definitions go in `pi/models.json` and its skills under `pi/skills/`. Use the harness's documentation for file formats.

These files synchronize when the container next starts. Finish attached commands before stopping a kept-running container to apply them. **Edit the host config directory for lasting changes**—Devbox overwrites its managed copies inside the container.

## Prepare the workspace

Add **setup.sh** for tasks needing the mounted project, such as installing dependencies. It runs during container creation and recreation. Changes to this script require recreation. Keep reusable tool installation in the Dockerfile instead.

## Run something before each launch

Add **before-open.sh** for work needed before every harness launch. It runs inside `/workspace`. Keep it quick.

Both script types run in config order. A failure stops the chain, but does not undo completed work. See [artifacts](../reference/configuration.md#artifacts) for the complete rules.

**Next:** [Networking](networking.md).
