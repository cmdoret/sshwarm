set positional-arguments
root_dir := justfile_directory()
out_dir := root_dir / ".output/nix"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Lint the code: the Go, and the probe's shell
lint *args:
  nix flake check "$@" "{{root_dir}}"

# Build nix outputs into .output/
build *args:
  nix build --print-build-logs -o "{{out_dir}}" "{{root_dir}}"

# Update flake lock
update *args:
  nix flake update "$@" --flake "{{root_dir}}"

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
