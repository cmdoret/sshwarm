set positional-arguments
set shell := ["bash", "-cue"]

root_dir := justfile_directory()
out_dir := root_dir / ".output/release"
nu_log := root_dir / "tools/nu/log.nu"
nu_cmd := root_dir / "tools/nu/nix.nu"
version_file := root_dir / "tools/nix/sshwarm-version.txt"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Build the release binaries + checksums.
build:
  @"{{nu_log}}" info "Building release binaries..."
  nix build --print-build-logs -o "{{out_dir}}/bin" "{{root_dir}}#release"
  @"{{nu_log}}" info "Writing checksums..."
  cd "{{out_dir}}/bin" && sha256sum sshwarm-* > "{{out_dir}}/sha256sums.txt"
  @"{{nu_log}}" info "Release ready:"
  @eza --short-nix --follow-symlinks -T1 "{{out_dir}}/"

# Set the version (x.y.z, or major, minor, patch), commit and tag it v<version>
bump version:
  #!/usr/bin/env bash
  set -euo pipefail
  # The version first, to set it only if its tag is free.
  v=$("{{nu_cmd}}" update-version --dry-run "$1" "{{version_file}}")
  if git rev-parse -q --verify "refs/tags/v$v" >/dev/null; then
    "{{nu_log}}" err "Tag v$v exists already"
    exit 1
  fi
  "{{nu_cmd}}" update-version "$v" "{{version_file}}"
  git commit -m "chore: bump v$v" -- "{{version_file}}"
  git tag -a "v$v" -m "v$v"
  "{{nu_log}}" info "Tagged v$v: git push --follow-tags, then publish its release"

# Fails if nix version differs from tag.
check-version tag:
  @version=$(nix eval --raw "{{root_dir}}#default.version"); \
    [ "${1#v}" = "$version" ] && \
    { "{{nu_log}}" info "Tag $1 == version $version"; } || \
    { "{{nu_log}}" err "Tag $1 != version $version: bump it with just release::bump"; exit 1; }

# Upload the binaries and their checksums to the GitHub release of a tag (gh auth)
upload tag: (check-version tag) build
  @"{{nu_log}}" info "Uploading binaries to release {{tag}}..."
  gh release upload "$1" "{{out_dir}}"/bin/* "{{out_dir}}/sha256sums.txt" --clobber
