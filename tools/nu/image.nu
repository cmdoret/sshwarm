#!/usr/bin/env -S nu --stdin

use lib/log.nu *

# Push the images `just image::build` streams, one per architecture, as
# <repo>:<tag>-<arch>, then each tag as the list of them all: docker pulls the one for
# its machine.
export def "main push" [
  out_dir: string  # the images' streams: <out_dir>/<arch>
  repo: string     # e.g. ghcr.io/cmdoret/sshwarm
  arches: string   # e.g. "amd64 arm64"
  ...tags: string  # e.g. 1.2.0 latest
] {
  if ($tags | is-empty) {
    log error "No tag to push"
    exit 1
  }

  let images = $arches | split row " " | each {|arch|
    let image = $"($repo):($tags.0)-($arch)"
    log info $"Pushing ($image)..."

    let loaded = ^$"($out_dir)/($arch)" | ^docker load | parse "Loaded image: {name}" | get 0.name
    ^docker tag $loaded $image
    ^docker push --quiet $image
    $image
  }

  for tag in $tags {
    let list = $"($repo):($tag)"
    log info $"Pushing ($list): ($images | str join ', ')..."
    ^docker manifest create --amend $list ...$images
    ^docker manifest push --purge $list
  }
}

def main [] {}
