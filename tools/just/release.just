set positional-arguments
set shell := ["bash", "-cue"]

root_dir := justfile_directory()
out_dir := root_dir / ".output/release"
nu_log := root_dir / "tools/nu/log.nu"

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

# Fails if nix version differs from tag.
check-version tag:
  @version=$(nix eval --raw "{{root_dir}}#default.version"); \
    [ "${1#v}" = "$version" ] && \
    { "{{nu_log}}" info "Tag $1 == version $version"; } || \
    { "{{nu_log}}" err "Tag $1 != version $version: bump it in tools/nix/sshwarm.nix"; exit 1; }

# Upload the binaries and their checksums to the GitHub release of a tag (gh auth)
upload tag: (check-version tag) build
  @"{{nu_log}}" info "Uploading binaries to release {{tag}}..."
  gh release upload "$1" "{{out_dir}}"/bin/* "{{out_dir}}/sha256sums.txt" --clobber
