# The binaries a release ships, one per machine: sshwarm-linux-amd64...
{
  lib,
  linkFarm,
  sshwarmFor,
}:
let
  targets = lib.cartesianProduct {
    goos = [
      "linux"
      "darwin"
    ];
    goarch = [
      "amd64"
      "arm64"
    ];
  };
in
# A directory of symlinks, one per { name, path }:
# sshwarm-linux-amd64 -> /nix/store/…-sshwarm-linux-amd64-1.0.0/bin/sshwarm
linkFarm "sshwarm-release" (
  map (t: {
    name = "sshwarm-${t.goos}-${t.goarch}";
    path = "${sshwarmFor t.goos t.goarch}/bin/sshwarm";
  }) targets
)
