#!/usr/bin/env bash
# Read-only probe that `sshwarm` pipes to each machine:
#   ssh <host> timeout <s> bash -s
# Prints sections: a "### <name>" line, then that command's output.
# A command that fails prints "### error <name>" and its message instead.
# Any Linux with systemd: the NixOS-only lines first check for what they read.
export LC_ALL=C

section() {
  name=$1
  shift
  if out=$("$@" 2>&1); then
    printf '### %s\n%s\n' "$name" "$out"
  else
    printf '### error %s\n%s\n' "$name" "$out"
  fi
}

# reboot_pending: the system installed isn't the one running.
reboot_pending() {
  if [ -e /run/booted-system ]; then # NixOS: another kernel, initrd or modules since boot
    for f in kernel initrd kernel-modules; do
      [ "$(readlink -f "/run/booted-system/$f")" = "$(readlink -f "/run/current-system/$f")" ] || return 0
    done
    return 1
  fi
  [ -e /var/run/reboot-required ] && return 0     # Debian, Ubuntu
  if command -v needs-restarting >/dev/null; then # Fedora, RHEL: exits 1 when a reboot is needed
    needs-restarting -r >/dev/null 2>&1
    [ $? -eq 1 ] && return 0
  fi
  # The running kernel was upgraded away (Arch). Containers have no modules at all.
  [ -n "$(ls /lib/modules 2>/dev/null)" ] && [ ! -d "/lib/modules/$(uname -r)" ]
}

# docker_cmd prints the first command that reaches docker (rootless or rootful).
docker_cmd() {
  command -v docker >/dev/null || return
  for d in docker "docker -H unix:///run/user/$(id -u)/docker.sock" "sudo -n docker"; do
    if $d ps -q >/dev/null 2>&1; then
      echo "$d"
      return
    fi
  done
  echo "sudo -n docker" # none works: the containers section shows its error
}

# container_times prints when each compose container last started and stopped,
container_times() {
  # shellcheck disable=SC2086 # words, split on purpose: "sudo -n docker"
  $docker ps -aq --filter label=com.docker.compose.project |
    xargs -r $docker inspect --format '{{.Name}} {{.State.StartedAt}} {{.State.FinishedAt}}'
}

echo "### meta"
# shellcheck source=/dev/null # the machine's
echo "os=$(. /etc/os-release && echo "$PRETTY_NAME")"
echo "now=$(date +%s)"
echo "uptime=$(cut -d' ' -f1 /proc/uptime)"
if link=$(readlink /nix/var/nix/profiles/system); then # NixOS: system-<generation>-link, made at deploy
  echo "deployed=$(stat -c %Y "/nix/var/nix/profiles/$link")"
fi
if rev=$(nixos-version --configuration-revision 2>/dev/null); then # NixOS, when the flake sets it
  echo "revision=$rev"
fi
echo "reboot=$(reboot_pending && echo true || echo false)"
# Usage in percent. The 1-minute load per CPU, past 100 when tasks wait: getconf
# counts the machine's CPUs, where nproc would obey OMP_NUM_THREADS.
echo "load=$(awk -v cpus="$(getconf _NPROCESSORS_ONLN)" '{ print 100 * $1 / cpus }' /proc/loadavg)"
# Memory in use: what isn't available to programs.
echo "memory=$(awk '/^MemTotal:/ { total = $2 } /^MemAvailable:/ { free = $2 } END { print 100 * (total - free) / total }' /proc/meminfo)"
# The fullest disk: local ones only, within 2s, as a hung mount would hang df and
# time the whole probe out.
echo "disk=$(timeout 2 df -P -l -x tmpfs -x devtmpfs -x overlay -x squashfs -x efivarfs 2>/dev/null |
  awk 'NR > 1 && $3 + $4 > 0 { used = 100 * $3 / ($3 + $4); if (used > disk) disk = used } END { print disk }')"
docker=$(docker_cmd)
echo "docker=$docker"

section units systemctl show --timestamp=unix --no-pager \
  -p Id,ActiveState,SubState,Description,Result,UnitFileState,StateChangeTimestamp '*.service'
if [ -n "$docker" ]; then
  # shellcheck disable=SC2086 # words, split on purpose: "sudo -n docker"
  section containers $docker ps -a --filter label=com.docker.compose.project \
    --format '{{.Label "com.docker.compose.project"}}\t{{.Names}}\t{{.State}}\t{{.Status}}'
  section times container_times
fi
