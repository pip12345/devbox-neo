# Devbox rewrite

An independent Go scratch rewrite. **In development, not ready for cutover.**

- [Design and delivery phases](docs/dev/rewrite-plan.md)
- [Separate migration plan](docs/dev/migration-plan.md)
- [Implementation progress](docs/dev/progress.md)

```sh
make install-go  # optional if the repo or parent already has .tools/go
make check
bin/devbox-rewrite --help
```

`make test-integration` requires a working local Docker daemon and uses isolated temporary homes and rewrite-only Docker resources. It must never target the existing installation.
