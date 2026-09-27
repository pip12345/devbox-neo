# Share SSH access

You authenticate in a host terminal. The agent reuses that connection without receiving your credentials. Keep the terminal open while sharing.

## Connect

Choose **SSH** in the session menu and enter a destination such as `user@server`. Or, from a project with a selected default, run on the host:

```sh
dbx ssh . user@server
```

Answer password, passphrase, MFA, and host-key prompts yourself. SSH runs inside the container by default, using its SSH configuration and network.

After authentication, Devbox prints the exact command the agent should use. Give it that exact command because some destinations receive a generated alias.

## Use the shared connection

For a published alias named `staging`, the agent can run inside the container:

```sh
ssh -F /devbox/ssh/config staging 'hostname'
```

File transfer works through the same config:

```sh
scp -F /devbox/ssh/config ./report.txt staging:/tmp/
```

Other destinations can be shared from separate host terminals.

## Disconnect

Press **Ctrl-C** in the host terminal. This closes the shared connection and its active SSH sessions.

If the connection drops, authenticate again from the host. Generated client commands fail rather than opening a fresh login.

## Use your host's SSH configuration

When your keys, aliases, or jump hosts are already configured on the host, choose **Use host SSH master** or run:

```sh
dbx ssh . staging --host-master
```

**Host mode grants broader access:** the container can act as your authenticated remote user and create tunnels into your host and reachable networks. Use it only when you intend to grant that access.

Normal OpenSSH settings, including `ProxyJump`, apply wherever the SSH master runs. See [SSH options](../reference/commands.md#ssh-sharing) for forwarding and destination rules.

**Next:** [Manage environments](managing-environments.md).
