<p align="center">
  <img src="assets/logo.svg" alt="" width="120">
</p>

<h1 align="center">sshwarm</h1>

<p align="center"><b><a href="https://k9scli.io">k9s</a> for ssh.</b> Your NixOS machines, their services, containers and logs, in one terminal.</p>

Tired of ssh-ing into box after box to find out what's deployed where, which service died
overnight and which container is crash-looping? **sshwarm** shows the whole swarm at a
glance, with the logs of whatever is red a keypress away.

- **Every machine, one table:** deployed commit, uptime, pending reboot, failed units, unhealthy compose stacks.
- **Agentless and read-only:** a small bash probe over your own `ssh`, with your keys, agent and `~/.ssh/config`. Nothing to install on the machines.
- **Scriptable:** `sshwarm status` prints the table and exits 1 when anything is wrong.

## Get it

```sh
nix run github:cmdoret/sshwarm -- --flake ./nix
go install github.com/cmdoret/sshwarm@latest
```

## Requirements

- `ssh`, `nix`, and a flake listing the machines in `deploy.nodes`, as for
  [deploy-rs](https://github.com/serokell/deploy-rs): `hostname` and `sshUser`.
- NixOS machines reachable over ssh without a password prompt (keys or agent) with:
    - ssh user in the `systemd-journal` group.
    - passwordless `sudo docker`.
    - optional: `system.configurationRevision` set in the machines' configuration.

## Use

```sh
sshwarm --flake ./nix              # browse: machines, then services and stacks, then logs
sshwarm --flake ./nix status web   # the table, for the machines whose name contains "web"
```

Set `SSHWARM_FLAKE` to skip `--flake`. The rest: `sshwarm help`.

<sub>GPL-3.0</sub>
