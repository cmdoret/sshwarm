set positional-arguments
set shell := ["bash", "-cue"]

root_dir := justfile_directory()
out_dir := root_dir / ".output/release"
nu_log := root_dir / "tools/nu/log.nu"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Build the binaries a release ships into .output/release, with their checksums
build:
  @"{{nu_log}}" info "Building release binaries..."
  nix build --print-build-logs -o "{{out_dir}}/bin" "{{root_dir}}#release"
  cd "{{out_dir}}/bin" && sha256sum sshwarm-* > "{{out_dir}}/sha256sums.txt"

# Upload the binaries and their checksums to the GitHub release of a tag (gh auth)
upload tag: build
  @"{{nu_log}}" info "Uploading binaries to release {{tag}}..."
  gh release upload "$1" "{{out_dir}}"/bin/* "{{out_dir}}/sha256sums.txt" --clobber
