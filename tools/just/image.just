set positional-arguments
set shell := ["bash", "-cue"]

root_dir := justfile_directory()
out_dir := root_dir / ".output/image"
nu_cmd := root_dir / "tools/nu/image.nu"
nu_log := root_dir / "tools/nu/log.nu"
repo := "ghcr.io/cmdoret/sshwarm"
arches := "amd64 arm64"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Build the images, one per architecture, into .output/image
build:
  @"{{nu_log}}" info "Building images for {{arches}}..."
  for arch in {{arches}}; do nix build --print-build-logs -o "{{out_dir}}/$arch" "{{root_dir}}#image-$arch"; done

# Push the images to ghcr.io as each tag given, e.g. 1.2.0 latest (docker login)
push +tags: build
  @"{{nu_cmd}}" push "{{out_dir}}" "{{repo}}" "{{arches}}" "$@"
