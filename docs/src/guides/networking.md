# Networking

A container has its own network view by default. The address you use depends on where the service runs: inside the environment, on your host, or in another container.

## Open a development server in your browser

Publish a port when creating the environment:

```sh
devbox-neo create . --port 127.0.0.1:8080:8080
```

Start your server inside the container, listening on `0.0.0.0:8080`. Then open `http://127.0.0.1:8080` in your host browser.

The first address and port belong to the host; the last port belongs to the container. Binding the host side to `127.0.0.1` keeps it local to your machine.

For an existing environment, add the mapping to `ports` in its profile or project configuration and run `recreate`. Publishing ports is a container setting; stopping and starting does not change it.

## Reach a service on your host

Inside the container, load Devbox's network variables:

```sh
source /devbox/network/env
curl "http://$DEVBOX_HOST:8080"
```

The host service must listen on an interface reachable from Docker. A service bound only to host `127.0.0.1` normally cannot be reached from a bridge-network container. Configure a reachable bind address and allow the connection through your firewall.

## Connect to another Docker container

If the other container uses an existing Docker network named `backend`, join it from the host:

```sh
devbox-neo network connect backend .
```

You can then use the other container's network name and service port. To leave:

```sh
devbox-neo network disconnect backend .
```

These extra attachments survive stop/start but are lost on recreation. If this should be your environment's primary network, set `network` to `backend` in its configuration instead. Devbox expects the network to exist.

## Inspect the connection

From the host:

```sh
devbox-neo network inspect .
devbox-neo network env . --get DEVBOX_DEFAULT_GATEWAY_IP
```

Inside the container, `/devbox/network/inspect.json` contains the inspected addresses, gateways, and attachments. Use those facts rather than guessing Docker subnet addresses.

## Use host networking when needed

Set `network` to `host` and recreate if your workflow needs the host's network namespace. This removes the normal network separation. Published ports and secondary Docker-network attachments are unavailable in this mode.

See the [network command reference](../reference/commands.md#networks) for syntax, or [SSH sharing](ssh.md) for authenticated access to remote machines.
