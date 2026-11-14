#!/usr/bin/env -S nu --stdin

use lib/log.nu *
use lib/viz.nu *
 
# Updates the vendor-hash in a text file based on nix build output.
export def "main update-hash" [
  flake_dir: string # Directory containing the nix flake to build
  hash_file: string # File storing the hash, must exist.
] {
  
  let spinner_id = job spawn { spinner  "Calculating hash..." }
  let res = (^nix build --no-link --impure $flake_dir | complete)
  job kill $spinner_id
  print "\n"

  if $res.exit_code == 0 {
    log warning "Hash already up to date"
    exit 0
  }

  let found = ($res.stderr | parse --regex 'got:\s*(?<hash>sha256-\S+)')
  if not ($found | is-empty) {
    $found.0.hash | save --force $hash_file
    log info "Hash updated"
    exit 0
  }
  log error $res.stderr
}
#
# Nix helper commands.
#
# Run `nix.nu <subcommand> --help` for details on each subcommand.
def main [] {}
