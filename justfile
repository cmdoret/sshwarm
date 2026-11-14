set positional-arguments
root_dir := justfile_directory()
nu_log := root_dir / "tools/nu/log.nu"

# Manage go package
[group("modules")]
mod go 'tools/just/go.just'
# Manage nix outputs
[group("modules")]
mod nix 'tools/just/nix.just'
# Manage release binaries
[group("modules")]
mod release 'tools/just/release.just'
# Manage container images
[group("modules")]
mod image 'tools/just/image.just'

# List the recipes
[private]
default:
  @just --list --unsorted

# Install git hooks
[group("tooling")]
setup:
  @"{{nu_log}}" info "Setting up git hooks..."
  prek install

# Run tests
[group("tooling")]
test: go::test

# Lint repo
[group("tooling")]
lint: go::lint nix::lint

# Build all outputs into .output
[group("tooling")]
build: go::build nix::build

# Update all lock files and the vendor hash.
[group("tooling")]
update: go::update nix::update

# Format repo
[group("tooling")]
format *args:
  @"{{nu_log}}" info "Formatting repository tree..."
  treefmt --config-file "{{root_dir}}/tools/config/treefmt.toml" "$@"

# Fail on unformatted files, formatting them
[private]
check-format *args:
  @just format --fail-on-change --no-cache "$@" || \
    { "{{nu_log}}" err "Unformatted files: run 'just format' locally."; exit 0; }

