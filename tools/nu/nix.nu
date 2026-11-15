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

# Sets the version in a text file, either from a semver string or bump type.
export def "main update-version" [
  version: string # The new version, e.g. 1.4.0 or bump (major|minor|patch)
  version_file: string # File storing the version, must exist.
  --dry-run # Print the version it would set, and set nothing
] {
  let old = (open --raw $version_file | str trim | into semver)
  let new = if $version in [major minor patch] {
    $old | semver bump $version
  } else {
    try { $version | str replace --regex '^v' '' | into semver } catch {
      log error $"Not a version: ($version), want x.y.z, or major, minor or patch"
      exit 1
    }
  }

  # It must come after the version there: catches a typo, and a bump that doesn't.
  if $new <= $old {
    log error $"Version ($new | into string) isn't after ($old | into string)"
    exit 1
  }

  if $dry_run {
    print ($new | into string)
    return
  }

  $new | into string | save --force $version_file
  log info $"Version ($old | into string) -> ($new | into string)"
}
#
# Nix helper commands.
#
# Run `nix.nu <subcommand> --help` for details on each subcommand.
def main [] {}
