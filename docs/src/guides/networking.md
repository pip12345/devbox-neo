# Networking

Choose the section for where your service runs. Commands marked “host” run outside the Devbox container.

## Open a development server in your browser

Run `dbx config` and open a config used by your session. Choose **Port forwards** → **Add port forward**:

```text
127.0.0.1:8080:8080
```

This maps host port `8080` to container port `8080`, accessible only from your host. Choose **Recreate** in the session menu to apply the mapping.

Start your server inside the container, listening on `0.0.0.0:8080`. Open `http://127.0.0.1:8080` in your host browser.

For a new session, configure the mapping before creating it. Port changes require recreation, not just stop/start.

## Reach a service on your host

Inside the container:

```sh
source /devbox/network/env
curl "http://$DEVBOX_HOST:8080"
```

The host service must listen on an interface Docker can reach. A host service bound only to `127.0.0.1` normally cannot be reached from a bridge-network container. Adjust its bind address and firewall as needed.

## Reach another container

Join an existing shared Docker network from the host. Here `.` selects your project folder's default session:

```sh
dbx network connect backend .
```

Use the other container's network name and service port. The attachment survives stop/start but not recreation. Set **Network** in your config if `backend` should be the primary network instead.

To detach, use `dbx network disconnect backend .`.

## Check the addresses

From the host, use **Networks** in the session menu or:

```sh
dbx network inspect .
```

Inside the container, read `/devbox/network/inspect.json`. Use these facts instead of guessing subnet addresses.

[Network reference](../reference/commands.md#networks) covers exports and host-network restrictions.

**Next:** [Share SSH access](ssh.md).
