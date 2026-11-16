# Architecture

```mermaid
flowchart LR
    src["hosts.toml<br>or flake"] --> sw[sshwarm]
    sw -- "ssh, every --refresh:<br>probe.sh on stdin" --> m["each machine"]
    m -- "### sections:<br>OS, usage, units, stacks" --> sw
    sw --> ui["browser,<br>or status"]
    ui -. "logs, describe,<br>shell, actions" .-> m
```

1. sshwarm reads the machines from a hosts file or a flake.
2. It pipes [`probe.sh`](https://github.com/cmdoret/sshwarm/blob/main/internal/swarm/probe.sh)
   to every machine over ssh, in parallel, and waits for all. The browser does it
   again `--refresh` after the last round ended.
3. The probe only reads: OS, uptime, usage, pending reboot, `systemctl show` of every
   service, `docker ps` and `docker inspect` of compose containers. It prints
   `### <name>` sections; a command that fails prints `### error <name>`.
4. sshwarm parses each machine's output into a report, and rates what's in it: idle,
   fine, warning or broken. A machine's problems are what's broken, what restarted on
   its own in the last 10 minutes, a disk 90% full, and the probe's errors.
   `status` exits 1 on any.
5. Logs, descriptions, shells and actions each run their own ssh command, on demand.

## In time

A refresh, a unit's logs, then a container's restart:

```mermaid
sequenceDiagram
    actor You
    participant UI as browser
    participant SW as swarm
    participant M as each machine

    loop every --refresh, from the end of the last round
        UI->>SW: probe
        SW->>M: ssh: bash, probe.sh on stdin, to all at once
        M-->>SW: OS, usage, units, containers, in sections
        SW-->>UI: a report per machine, rated
    end

    You->>UI: l, on a unit
    UI->>M: ssh: journalctl -u unit -n 300
    M-->>UI: its last lines
    Note over UI,M: s follows them: journalctl -f, with a terminal

    You->>UI: R, on a container
    UI-->>You: restart container web-1 on app-1? y/N
    You->>UI: y
    UI->>M: ssh: docker restart web-1
    UI->>SW: probe again
```

## Code

| | |
|---|---|
| `main.go` | flags, commands |
| `internal/inventory` | hosts file, flake |
| `internal/remote` | ssh: to the end, as a stream, or with the terminal; shared connections |
| `internal/swarm` | the probe and its parsing, the report, the commands for logs, descriptions and actions |
| `internal/ui` | the browser: screens, tables, cards, keys; `status`'s table and JSON |

## Choices

- **Agentless.** A bash script over the user's own ssh: nothing to install, and
  their ssh config, keys and agent just work.
- **Read-only probe.** Actions ask first, and `--read-only` turns them off.
- **The machine's clock.** Ages are computed with the machine's time, not yours.
- **Bounded waits.** The probe stops itself on the machine before sshwarm gives up
  on it, and gives `df` 2 s, as a hung mount would hang it.
