#!/usr/bin/env -S nu --stdin

use lib/log.nu *
use lib/viz.nu *

# Updates the vendor-hash in a text file based on nix build output.
#
# 1. build Go dependencies with a placeholder hash -> nix downloads them.
# 2. Get new hash from mismatch error.
export def "main update-hash" [
  flake_dir: string # Directory containing the nix flake to build
  hash_file: string # File storing the hash, must exist.
] {
  let modules = (
    'let pkg = (builtins.getFlake "' + $flake_dir + '").packages.${builtins.currentSystem}.default;'
    + ' in (pkg.overrideAttrs { vendorHash = ""; }).goModules'
  )

  let spinner_id = job spawn { spinner  "Calculating hash..." }
  let res = (^nix build --no-link --impure --expr $modules | complete)
  job kill $spinner_id
  print "\n"

  let found = ($res.stderr | parse --regex 'got:\s*(?<hash>sha256-\S+)')
  if ($found | is-empty) {
    log error $res.stderr
    exit 1
  }

  if $found.0.hash == (open --raw $hash_file | str trim) {
    log warning "Hash already up to date"
    exit 0
  }

  $found.0.hash | save --force $hash_file
  log info "Hash updated"
}
#
# Nix helper commands.
#
# Run `nix.nu <subcommand> --help` for details on each subcommand.
def main [] {}
