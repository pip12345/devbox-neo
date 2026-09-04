# Trying the development rewrite

This is a Linux-only development build. The full rewrite is not complete; see [implementation progress](../../dev/progress.md). Use a non-root account with access to Docker.

## Build

From the rewrite repository:

```sh
make check
```

The binary is `bin/devbox-rewrite`. Its default home is `~/.devbox-neo`; it uses separate Docker names and labels. `--home` overrides `DEVBOX_HOME`, which overrides the default. The old `~/.devbox` is rejected, including when an inherited environment variable selects it.

## Configure the first profile

Create `~/.devbox-neo/profiles/basic/config.json` with:

```json
{"version": 1, "harness": "pi"}
```

This early runtime path uses direct configuration files. Profile/project create/init commands and dashboards are not implemented yet. No profile or harness is selected implicitly.

Open a workspace:

```sh
bin/devbox-rewrite /path/to/workspace --profile basic
```

The first open builds the image and starts Pi. Later opens reuse the container. The default `on_exit` policy stops it after the last attached Devbox command exits.

For a non-interactive launch check without provider credentials:

```sh
bin/devbox-rewrite /path/to/workspace --profile basic -- --version
```

## Apply changes explicitly

Valid creation changes warn instead of replacing the existing container:

```sh
bin/devbox-rewrite recreate /path/to/workspace --profile basic
bin/devbox-rewrite recreate /path/to/workspace --profile basic --image
```

`--image` disables build cache for the selected session. It does not promise to refresh upstream base images. Durable harness state is preserved.

Managed config is synchronized only while stopped. If an open reports deferred configuration, stop the container when safe and open it again.

## Access without desired configuration

```sh
bin/devbox-rewrite start /path/to/workspace --profile basic
bin/devbox-rewrite shell /path/to/workspace --profile basic
bin/devbox-rewrite exec /path/to/workspace --profile basic -- git status
bin/devbox-rewrite stop /path/to/workspace --profile basic
```

For existing containers these commands use the recorded contract, not current profile/project config. `shell` and `exec` require an existing container. A missing container can be recovered by `start` only if its recorded inputs remain available and unchanged; otherwise use explicit `recreate`.

Secrets belong in auth or supported environment inputs, not launch arguments, raw Docker arguments, or ordinary settings. Host environment configuration/substitution is not implemented in this checkpoint and is rejected rather than used literally. Managed Pi auth persists at `~/.devbox-neo/auth/pi/auth.json`.
