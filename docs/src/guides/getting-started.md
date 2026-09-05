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

Re-running init keeps existing files. The current optional artifacts are harness config, `setup.sh`, and `entrypoint.sh`; Dockerfile seeding and dashboards remain pending.

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

Secrets belong in auth or supported environment inputs, not launch arguments, raw Docker arguments, or ordinary settings. Host environment configuration/substitution is not implemented in this checkpoint and is rejected rather than used literally. Managed Pi and OpenCode auth persist at `~/.devbox-neo/auth/<harness>/auth.json`.
