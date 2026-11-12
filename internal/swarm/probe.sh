# Read-only probe that `sshwarm` pipes to each machine:
#   ssh <host> timeout <s> bash -s -- '<docker command>'   e.g. 'sudo -n docker'
# Prints sections: a "### <name>" line, then that command's output.
# A command that fails prints "### error <name>" and its message instead.
export LC_ALL=C
docker_cmd=${1:-docker} # words, split on purpose below

section() {
  name=$1
  shift
  if out=$("$@" 2>&1); then
    printf '### %s\n%s\n' "$name" "$out"
  else
    printf '### error %s\n%s\n' "$name" "$out"
  fi
}

echo "### meta"
echo "now=$(date +%s)"
echo "uptime=$(cut -d' ' -f1 /proc/uptime)"
if link=$(readlink /nix/var/nix/profiles/system); then # system-<generation>-link
  echo "generation=$(echo "$link" | tr -dc 0-9)"
  echo "deployed=$(stat -c %Y "/nix/var/nix/profiles/$link")"
fi
reboot=false
for f in kernel initrd kernel-modules; do
  [ "$(readlink -f "/run/booted-system/$f")" = "$(readlink -f "/run/current-system/$f")" ] || reboot=true
done
echo "reboot=$reboot"
docker=false
command -v docker >/dev/null && docker=true
echo "docker=$docker"

section version nixos-version --json
section units systemctl list-units --type=service --all --output=json --no-pager
section show systemctl show --timestamp=unix --all --no-pager \
  -p Id,Result,UnitFileState,StateChangeTimestamp '*.service'
if [ "$docker" = true ]; then
  section containers $docker_cmd ps -a --filter label=com.docker.compose.project \
    --format '{{.Label "com.docker.compose.project"}}\t{{.Names}}\t{{.State}}\t{{.Status}}'
fi
