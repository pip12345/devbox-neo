# Share SSH access

SSH sharing lets you handle login in a host terminal while your agent reuses the authenticated connection. Devbox does not copy your credentials.

Start with an [existing environment](getting-started.md). You'll need a host terminal you can leave open for the connection's lifetime.

## 1. Connect and authenticate

Run on the host:

```sh
devbox-neo ssh . user@server
```

Answer any password, passphrase, MFA, or host-key prompts yourself. SSH runs inside the container by default, using its network and SSH configuration.

After authentication, Devbox prints the exact command the agent can use. Give that command to your agent; destinations such as `user@server` receive a generated alias.

## 2. Use the connection

For an SSH alias named `staging`, the workflow looks like this:

```sh
# On the host
devbox-neo ssh . staging

# Inside the container, after authentication
ssh -F /devbox/ssh/config staging 'hostname'
scp -F /devbox/ssh/config ./report.txt staging:/tmp/
```

Keep the host terminal open. You can share other destinations from separate terminals.

## 3. Disconnect

Press Ctrl-C in the host terminal. Devbox closes the connection and its active SSH sessions, then prints `Disconnected.` after cleanup.

If the connection drops, run the host command again. The generated client command fails when the shared connection is unavailable; it does not start a fresh login.

## Use your host's SSH setup

If your keys, aliases, agent, or jump-host configuration are already set up on the host, opt into host mode:

```sh
devbox-neo ssh . staging --host-master
```

SSH now runs on the host, with its normal configuration. The flag applies only to this connection.

**Host mode grants broader access:** the container can act as your authenticated remote user and create tunnels into your computer and networks it can reach. Devbox warns before authentication. Use it only when you're comfortable granting that access to this environment.

## Configure a jump host

Use normal OpenSSH configuration wherever SSH runs: in the container for default mode, or on the host for `--host-master`.

```sshconfig
Host staging
    HostName staging.example.com
    User developer
    ProxyJump bastion

Host bastion
    HostName bastion.example.com
    User developer
```

Run `devbox-neo ssh . staging` with the appropriate mode and answer both hops' prompts in your terminal. `ProxyJump` does not require agent forwarding.

Container-local SSH files are lost on recreation unless you arrange to persist them. Host mode is useful when you want to use an existing host setup rather than maintain a second one.

For destination rules, forwarding, and connection lifetime, see the [SSH reference](../reference/commands.md#ssh-sharing).
