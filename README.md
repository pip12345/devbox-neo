# Devbox rewrite

An independent, Linux-only Go scratch rewrite. **In development, not ready for cutover.**

- [Design and delivery phases](docs/dev/rewrite-plan.md)
- [Separate migration plan](docs/dev/migration-plan.md)
- [Implementation progress](docs/dev/progress.md)
- [Try the initial runtime](docs/src/guides/getting-started.md)

```sh
make install-go  # optional if the repo or parent already has .tools/go
make check
bin/devbox-neo --help
```

The development home defaults to `~/.devbox-neo`. `--home` overrides `DEVBOX_HOME`, which overrides that default. Selecting the old `~/.devbox` (or anything inside it) is rejected, including symlink aliases. Docker names and ownership labels also stay separate from the existing installation.

`make test-integration` requires a working local Docker daemon and uses isolated temporary homes and rewrite-only Docker resources. It must never target the existing installation.
