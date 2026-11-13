set positional-arguments
root_dir := justfile_directory()
build_dir := root_dir / ".output/build"

# List the recipes
[private]
default:
  @just -f {{source_file()}} --list --unsorted

# Run the tests
test *args:
  go test {{args}} {{root_dir}}/...

# Lint the code: the Go, and the probe's shell
lint *args:
  go vet {{args}} {{root_dir}}/...
  shellcheck {{root_dir}}/internal/swarm/probe.sh

# Build sshwarm into .output/build
build *args:
  go build -o {{build_dir}} {{args}} {{root_dir}}
