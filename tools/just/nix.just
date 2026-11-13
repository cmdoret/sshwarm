set positional-arguments
set shell := ["bash", "-cue"]

root_dir := justfile_directory()
out_dir := root_dir / ".output/nix"
nu_cmd := root_dir / "tools/nu/nix.nu"
nu_log := root_dir / "tools/nu/log.nu"
vendor_hash := root_dir / "tools/nix/sshwarm-hash.txt"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Lint the code: the Go, and the probe's shell
lint *args:
  @"{{nu_log}}" info "Linting nix flake..."
  nix flake check "$@" "{{root_dir}}"

# Build nix outputs into .output/
build *args:
  @"{{nu_log}}" info "Building nix package..."
  nix build --print-build-logs -o "{{out_dir}}" "{{root_dir}}"

# Update flake lock and vendorHash
update *args: && update-vendor-hash
  @"{{nu_log}}" info "Updating nix lock..."
  nix flake update "$@" --flake "{{root_dir}}"

# Set flake.nix's vendorHash to what Go's dependencies hash.
[private]
update-vendor-hash:
  @"{{nu_log}}" info "Updating vendor hash..."
  @"{{nu_cmd}}" update-hash "{{root_dir}}" "{{vendor_hash}}"
    

# Exec cmd in shell
[no-cd]
develop shell="default" +cmd=env("SHELL", "bash"):
  nix develop \
    "{{root_dir}}#{{shell}}" \
    --command {{cmd}}

# Start an Nix interpreter with loaded flake.
[no-cd]
repl *args:
  nix repl "$@" "{{root_dir}}"
