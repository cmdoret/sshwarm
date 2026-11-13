set positional-arguments
root_dir := justfile_directory()

# Manage go package
[group("modules")]
mod go 'tools/just/go.just'
# Manage nix outputs
[group("modules")]
mod nix 'tools/just/nix.just'

# List the recipes
[private]
default:
  @just --list --unsorted

# Install git hooks
setup:
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

# Format repo
[group("tooling")]
format *args:
  treefmt --config-file "{{root_dir}}/tools/config/treefmt.toml" "$@"

# Fail on unformatted files, formatting them
[private]
check-format *args:
  just format --fail-on-change --no-cache "$@" || \
    { echo "Unformatted files: run 'just format' locally."; exit 1; }

