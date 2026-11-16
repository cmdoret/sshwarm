<p align="center"><img src="assets/logo.svg" alt="sshwarm" width="120"></p>

<p align="center"><b><a href="https://k9scli.io">k9s</a> for ssh.</b> Your Linux machines, their
services, containers and logs, in one terminal: what's deployed where, what died
overnight, what's crash-looping, and the logs of whatever is red a keypress away.</p>

# Quickstart

## Install

| From | |
|---|---|
| Go | `go install github.com/cmdoret/sshwarm@latest` |
| Nix | `nix run github:cmdoret/sshwarm` |
| Binary | [releases](https://github.com/cmdoret/sshwarm/releases): Linux and macOS, amd64 and arm64 |
| Container | [see below](#container) |

## List the machines

In `~/.config/sshwarm/hosts.toml`, one `[[host]]` per machine:

```toml
[[host]]
name = "db"                  # ssh db: the rest from ~/.ssh/config

[[host]]
name = "web-1"
host = "web-1.example.org"   # ssh -i ~/.ssh/web deploy@web-1.example.org
user = "deploy"
key  = "~/.ssh/web"
```

Using deploy-rs? Skip the file: `sshwarm --flake ./nix`. See [configuration](configuration.md).

## Run

```sh
sshwarm                       # browse
sshwarm status                # print the table, exit 1 on any problem
sshwarm status --format json  # for scripts
```

The machines need systemd, bash, and ssh without a password prompt. Logs and
containers need more: see [requirements](configuration.md#requirements).

## Keys

| Key | |
|---|---|
| `enter` | open |
| `l`, `d` | logs, describe |
| `/` | filter; in logs, search |
| `:ctr web` | containers of `web`; `:ctr all`, of every machine. `tab` completes |
| `?` | every key |

## Container

```sh
docker run --rm -it \
  -v ./hosts.toml:/hosts.toml:ro \
  -v ~/.ssh/id_ed25519:/root/.ssh/id_ed25519:ro \
  -v ~/.ssh/known_hosts:/root/.ssh/known_hosts:ro \
  ghcr.io/cmdoret/sshwarm --hosts /hosts.toml
```

No nix inside, so no `--flake`. ssh refuses a mounted `~/.ssh/config`: the hosts
file must set `host`, `user` and `key`.
