# Read-only probe that `sshwarm` pipes to each machine:
#   ssh <host> timeout <s> bash -s -- '<docker command>'   e.g. 'sudo -n docker'
# Prints sections: a "### <name>" line, then that command's output.
# A command that fails prints "### error <name>" and its message instead.
# Any Linux with systemd: the NixOS-only lines first check for what they read.
export LC_ALL=C
docker_cmd=$1 # words, split on purpose below

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
  [ -e /var/run/reboot-required ] && return 0 # Debian, Ubuntu
  if command -v needs-restarting >/dev/null; then # Fedora, RHEL: exits 1 when a reboot is needed
    needs-restarting -r >/dev/null 2>&1
    [ $? -eq 1 ] && return 0
  fi
  # The running kernel was upgraded away (Arch). Containers have no modules at all.
  [ -n "$(ls /lib/modules 2>/dev/null)" ] && [ ! -d "/lib/modules/$(uname -r)" ]
}

echo "### meta"
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
docker=false
command -v docker >/dev/null && docker=true
echo "docker=$docker"

section units systemctl show --timestamp=unix --no-pager \
  -p Id,ActiveState,SubState,Description,Result,UnitFileState,StateChangeTimestamp '*.service'
if [ "$docker" = true ]; then
  section containers $docker_cmd ps -a --filter label=com.docker.compose.project \
    --format '{{.Label "com.docker.compose.project"}}\t{{.Names}}\t{{.State}}\t{{.Status}}'
fi
