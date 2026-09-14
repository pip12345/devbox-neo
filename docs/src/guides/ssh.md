# Share an SSH connection with your agent

Run this on the host, in a terminal you can leave open:

```sh
devbox-neo ssh . user@server
```

Devbox starts an SSH master inside the existing environment. Answer password, passphrase, MFA, and host-key prompts in your host terminal. After authentication, the terminal prints the exact command the agent can use. The agent reuses your connection without handling the login.

For example, when the destination is the SSH config alias `staging`:

```sh
# Host terminal
devbox-neo ssh . staging

# Inside the container, after authentication
ssh -F /devbox/ssh/config staging 'hostname'
scp -F /devbox/ssh/config ./report.txt staging:/tmp/
```

Simple SSH aliases retain their names. Destinations containing a username or IPv6 address get a generated `ssh-<hash>` alias; use the command printed by Devbox. Separate destinations can be connected in separate host terminals. A duplicate active destination is refused.

Press Ctrl-C in the host terminal to end that connection and its active SSH sessions. After cleanup, Devbox restores your terminal, prints `Disconnected.`, and exits successfully. Authentication, connection, and cleanup failures still report errors. The command holds an attached-command lease, so ordinary stop/recreate/delete/transfer protections apply. The last attached command's `on_exit` policy still decides whether the container stops. Connection loss is an error, not an automatic reconnect. If access is unavailable, rerun the host command; the generated client config never falls back to interactive authentication or a direct connection.

Use `--profile NAME` or an exact environment name when the folder has multiple environments. Devbox starts a stopped environment through its normal startup/config synchronization path. It does not create a new environment. Existing environments without the socket mount require one explicit `recreate` before SSH sharing can be used.

## SSH configuration and jump hosts

Container mode reads the container user's normal SSH configuration, typically `/home/devuser/.ssh/config`. Configure ports, keys, and jump hosts there using OpenSSH syntax:

```sshconfig
Host staging
    HostName 192.168.1.50
    User developer
    ProxyJump bastion

Host bastion
    HostName bastion.example.com
    User developer
```

You answer authentication prompts for both hops in the host terminal. `ProxyJump` does not require SSH agent forwarding. Agent and X11 forwarding follow your normal SSH configuration; Devbox does not force them off. Container-side X11 forwarding requires a display available to the container, and agent forwarding requires an agent available there. To request forwarding for a reused session, pass `-A` or `-X`/`-Y` to the in-container SSH command; the master's SSH configuration must also permit that forwarding. The generated client config does not copy forwarding preferences from the master's config.

Devbox does not copy host keys/config, provision keys, or expose your host SSH agent automatically. There is no `-i`/`--identity` flag. A separately configured container key can authenticate independently of the master; disconnecting does not revoke that credential. Undeclared container-local SSH configuration and keys do not survive recreation or transfer.

Docker's default bridge normally allows outbound SSH to LAN and public servers without published container ports. Firewalls, VPN routes, and DNS must still allow the connection. Use a LAN IP if a local hostname does not resolve. Container mode uses the container's network namespace; Docker `network: host` removes that network separation.

## Use host configuration and credentials

Opt in per connection when you want the host's SSH aliases, keys, agent, or display:

```sh
devbox-neo ssh . staging --host-master
```

The dedicated master now runs on the host. Its socket is shared only with the selected environment; Devbox never adopts an unrelated personal master. This warning appears before authentication:

```text
WARNING: Running in host SSH master mode

The container can act as your authenticated SSH user on the remote
server. It can also create tunnels into your computer and networks
your computer can reach, potentially exposing private services
and data outside the container.
```

The connected status remains labelled `HOST MASTER`. No extra confirmation is required. Devbox does not edit host SSH configuration; OpenSSH's ordinary authentication and known-host behavior still applies. The flag is not remembered, and failed container authentication never switches modes automatically.

SSH sharing has no detached mode, saved credential store, reconnect daemon, or separate disconnect command. Clone/relocate do not copy SSH connections. Access to a connection is not permission for unrelated remote changes.
