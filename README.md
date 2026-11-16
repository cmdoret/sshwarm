<p align="center">
  <img src="docs/assets/logo.svg" alt="" width="120">
</p>

<h1 align="center">sshwarm</h1>

<p align="center"><b><a href="https://k9scli.io">k9s</a> for ssh.</b> Your Linux machines, their services, containers and logs, in one terminal.</p>

<p align="center">
  <a href="https://github.com/cmdoret/sshwarm/actions/workflows/checks.yml"><img src="https://img.shields.io/github/actions/workflow/status/cmdoret/sshwarm/checks.yml?branch=main&amp;label=checks" alt="checks"></a>
  <a href="https://github.com/cmdoret/sshwarm/releases/latest"><img src="https://img.shields.io/github/v/release/cmdoret/sshwarm" alt="release"></a>
  <a href="https://cmdoret.net/sshwarm"><img src="https://img.shields.io/badge/docs-cmdoret.net%2Fsshwarm-blue" alt="docs"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/cmdoret/sshwarm" alt="license"></a>
</p>

<p align="center"><a href="https://asciinema.org/a/jHeN9sWGwwmMRVq2" target="_blank"><img src="docs/assets/sshwarm.gif" alt="" width="720"></a></p>

Tired of ssh-ing into box after box to find out what's deployed where, which service died
overnight and which container is crash-looping? **sshwarm** shows the whole swarm at a
glance, with the logs of whatever is red a keypress away.

- **In depth overview:** uptime, pending reboot, systemd units, compose stacks, system resources.
- **Agentless:** a small bash probe over your own `ssh`, with your keys, agent and `~/.ssh/config`. Nothing to install on the machines.
- **Quick actions:** start/stop services or containers, or reboot a machine.
- **Scriptable:** get a json summary of all machines.

## Get it

```sh
go install github.com/cmdoret/sshwarm@latest
```

Or `nix run github:cmdoret/sshwarm`, a [release](https://github.com/cmdoret/sshwarm/releases)
binary, or the [container image](docs/quickstart.md#container).

## Use

```sh
sshwarm --hosts hosts.toml   # one [[host]] per machine, with its name = "web-1"
```

Then `?` lists the keys.

## Docs

[Quickstart](docs/quickstart.md) · [Configuration](docs/configuration.md) ·
[Architecture](docs/architecture.md) · [Contributing](docs/CONTRIBUTING.md), also at
[cmdoret.net/sshwarm](https://cmdoret.net/sshwarm).
