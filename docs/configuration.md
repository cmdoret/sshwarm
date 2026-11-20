# Configuration

sshwarm takes one file with the machine inventory.
Its command arguments can also be specified through environment variables.

## Machines

sshwarm takes the first of:

1. `--hosts <file>` or `--flake <dir>`
2. `SSHWARM_HOSTS` or `SSHWARM_FLAKE`
3. `$XDG_CONFIG_HOME/sshwarm/hosts.toml`, by default `~/.config/sshwarm/hosts.toml`

Flake and host file are mutually exclusive.

### Hosts file

```toml
[[host]]
name = "web-1"               # required: sshwarm's name for it
host = "web-1.example.org"   # default: name
user = "deploy"              # default: ~/.ssh/config's, else yours
key  = "~/.ssh/web"          # default: ~/.ssh/config's, or your agent
```

ssh looks `host` (or `name`) up in `~/.ssh/config` for anything else: port, jump
host... Unknown keys are an error.

### Nix flake

```sh
sshwarm --flake ./nix
```

Reads the flake's `deploy.nodes`, as [deploy-rs](https://github.com/serokell/deploy-rs)
does: each node's `hostname`, and its `sshUser` and `sshOpts`, or `deploy`'s. Needs `nix`.

## Flags

| Flag | Variable | Default | |
|---|---|---|---|
| `--hosts <file>` | `SSHWARM_HOSTS` | | hosts file |
| `--flake <dir>` | `SSHWARM_FLAKE` | | flake with `deploy.nodes` |
| `--timeout <duration>` | `SSHWARM_TIMEOUT` | `20s` | probe timeout, per machine |
| `--refresh <duration>` | `SSHWARM_REFRESH` | `30s` | how often the browser probes |
| `--read-only` | `SSHWARM_READ_ONLY` | off | no actions, no shells |

flags take precedence over environment variables. `SSHWARM_READ_ONLY` is on unless empty, `0` or `false`.

Commands: `status [--format text|json] [machine...]`, and `probe <machine>`, which
prints a machine's raw probe output. `sshwarm <command> --help` for more.

## Requirements

Each machine runs Linux with systemd and bash. ssh must log in without a prompt:
a key or an agent, and a known host key.

| To | the ssh user |
|---|---|
| see the machine and its units | needs nothing more |
| read logs | is root, or in `systemd-journal` |
| see, start and stop compose stacks and containers | runs `docker`: docker group, rootless, or passwordless `sudo` |
| start and stop units, reboot | is root, or runs `systemctl` with passwordless `sudo` |

Pending reboots are detected on NixOS, Debian, Ubuntu, Fedora, RHEL and Arch. On
NixOS, sshwarm also shows the last deploy and its git revision, if the flake sets
`system.configurationRevision`.

## Connections

The browser keeps one ssh connection per machine (`ControlMaster`), its socket in
`~/.ssh`, closed a minute after you quit. It leaves ssh alone if your ssh config
sets `ControlPath` or `ControlMaster` for the machine, or `~/.ssh` isn't writable.
