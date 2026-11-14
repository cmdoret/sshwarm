set positional-arguments
root_dir := justfile_directory()
out_dir := root_dir / ".output/go/"
nu_log := root_dir / "tools/nu/log.nu"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Run the tests
test *args:
  @"{{nu_log}}" info "Testing go code..."
  go test "$@" "{{root_dir}}/..."

# Lint the code: the Go, and the probe's shell
lint *args:
  @"{{nu_log}}" info "Linting go code..."
  go vet "$@" "{{root_dir}}/..."
  @"{{nu_log}}" info "Linting shell scripts..."
  shellcheck "{{root_dir}}/internal/swarm/probe.sh"

# Update the Go dependencies to their latest minor and patch versions
update *args:
  @"{{nu_log}}" info "Updating go dependencies..."
  cd "{{root_dir}}" && go get -u "$@" ./... && go mod tidy

# Build sshwarm into .output/build
build *args:
  @"{{nu_log}}" info "Building go code..."
  go build -o "{{out_dir}}" "$@" "{{root_dir}}"
