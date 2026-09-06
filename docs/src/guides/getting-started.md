# Trying the development rewrite

This is a Linux-only development build. The full rewrite is not complete; see [implementation progress](../../dev/progress.md). Use a non-root account with access to Docker.

## Build

From the rewrite repository:

```sh
make check
```

The binary is `bin/devbox-neo`. Its default home is `~/.devbox-neo`; it uses separate Docker names and labels. `--home` overrides `DEVBOX_HOME`, which overrides the default. The old `~/.devbox` is rejected, including when an inherited environment variable selects it.

## Configure the first profile

```sh
bin/devbox-neo profile create basic
bin/devbox-neo profile init basic --harness pi
bin/devbox-neo profile set basic
```

`create` writes sparse config without choosing a harness. `init` selects the harness; `set` makes the profile the default. No defaults are selected implicitly. Use `--harness opencode` for OpenCode.

In a terminal, `init` without flags offers harness and optional artifact choices. For automation, use explicit flags:

```sh
bin/devbox-neo profile init basic --harness pi --artifact harness-config,setup.sh
bin/devbox-neo profile list
```

Re-running init keeps existing files. Optional artifacts are harness config, `setup.sh`, `entrypoint.sh`, and `Dockerfile`. Dashboards remain pending.

Open a workspace:

```sh
bin/devbox-neo /path/to/workspace --profile basic
```

The first open builds the image and starts Pi. Later opens reuse the container. The default `on_exit` policy stops it after the last attached Devbox command exits.

For a non-interactive launch check without provider credentials:

```sh
bin/devbox-neo /path/to/workspace --profile basic -- --version
```

## Configure a project instead

```sh
bin/devbox-neo project create /path/to/workspace
bin/devbox-neo project init /path/to/workspace --harness pi
bin/devbox-neo /path/to/workspace
```

Use `--harness inherit` on project init to use the participating profile/global harness. Inheritance must resolve to a configured harness.

To start a standalone project from a reusable profile:

```sh
bin/devbox-neo project create /path/to/workspace --from-profile basic
```

This copies the profile's supported source artifacts once and sets `inherit_profile: false`. It preserves variable expressions, refuses an existing `.devbox/`, and does not track later profile changes. Global defaults still apply. Use `profile set --clear` to clear the default profile.

## Customize the image

```sh
bin/devbox-neo profile init basic --artifact Dockerfile
```

Edit the generated Dockerfile to add tools to a Debian-compatible base. Its directory is the build context, so `COPY` can use sibling files. Use `.dockerignore` to exclude files that are not image inputs. Devbox always installs its runtime and harness afterward; there is no full override mode. Init never replaces an existing Dockerfile.

## Apply changes explicitly

Valid creation changes warn instead of replacing the existing container:

```sh
bin/devbox-neo recreate /path/to/workspace --profile basic
bin/devbox-neo recreate /path/to/workspace --profile basic --image
```

`--image` disables build cache for the selected session. It does not promise to refresh upstream base images. Durable harness state is preserved.

Managed config is synchronized only while stopped. If an open reports deferred configuration, stop the container when safe and open it again.

## Access without desired configuration

```sh
bin/devbox-neo start /path/to/workspace --profile basic
bin/devbox-neo shell /path/to/workspace --profile basic
bin/devbox-neo exec /path/to/workspace --profile basic -- git status
bin/devbox-neo stop /path/to/workspace --profile basic
```

For existing containers these commands use the recorded contract, not current profile/project config. `shell` and `exec` require an existing container. A missing container can be recovered by `start` only if its recorded inputs remain available and unchanged; otherwise use explicit `recreate`.

## Environment and other creation settings

Profile/project config can reference the host environment:

```json
{
  "version": 1,
  "harness": "pi",
  "extra_env": ["WORK_TOKEN=${env:WORK_TOKEN}"],
  "extra_mounts": ["${env:HOME}/data:/data:ro"],
  "extra_ports": ["127.0.0.1:8080:8080"]
}
```

References expand once per operation; unset references are errors. Env values are sensitive. Paths, names, argv, and other ordinary settings are public, so do not put credentials there. Env values must be single-line. Managed Pi/OpenCode auth persists at `~/.devbox-neo/auth/<harness>/auth.json`.

Inspect effective values and provenance without starting Docker:

```sh
bin/devbox-neo profile config basic --show
bin/devbox-neo project config /path/to/workspace --show --json
```

Env values are redacted. Global/profile/project env can be recovered from verified source entries after container loss. One-off `--env KEY=VALUE` has no durable source: use explicit `recreate --env KEY=VALUE` after container loss. Existing-container start/shell/exec do not need the old env inputs.
