set positional-arguments
root_dir := justfile_directory()
book_dir := root_dir / "docs"
nu_log := root_dir / "tools/nu/log.nu"

# List the recipes
[private]
default:
  @just -f "{{source_file()}}" --list --unsorted

# Build the documentation website into .output/docs
build *args:
  @"{{nu_log}}" info "Building the documentation..."
  mdbook build "$@" "{{book_dir}}"

# Serve the documentation website, rebuilt on change, and open it
serve *args:
  mdbook serve --open "$@" "{{book_dir}}"
