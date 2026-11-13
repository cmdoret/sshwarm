# sshwarm, for this machine.
{ buildGoModule, src }:
buildGoModule {
  pname = "sshwarm";
  version = "1.0.0";
  inherit src;
  # `just nix::update` keeps it up to date.
  vendorHash = builtins.readFile ./sshwarm-hash.txt;
  # No cgo: a static binary, which runs on any machine of its kind, Nix or not.
  env.CGO_ENABLED = 0;
  meta.mainProgram = "sshwarm";
}
