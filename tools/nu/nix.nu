#!/usr/bin/env -S nu --stdin

use lib/log.nu *
use lib/viz.nu *
  
export def "main update-hash" [
  flake_dir: string
  hash_file: string
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

def main [] {}
