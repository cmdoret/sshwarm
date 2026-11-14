# sshwarm, for this machine.
{
  lib,
  buildGoModule,
  src,
}:
let
  version = "1.1.1";
  rev = src.rev or src.dirtyRev or "unknown";
in
buildGoModule {
  pname = "sshwarm";
  inherit version src;
  vendorHash = builtins.readFile ./sshwarm-hash.txt;
  # No cgo: a static binary, which runs on any machine of its kind (no links to nix store).
  env.CGO_ENABLED = 0;
  # Stripped, and stamped for `sshwarm --version`.
  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
    "-X main.commit=${rev}"
  ];
  # Used by the image's labels.
  passthru = { inherit rev; };
  meta = {
    description = "k9s for ssh: your Linux machines, their services, containers and logs, in one terminal";
    homepage = "https://github.com/cmdoret/sshwarm";
    changelog = "https://github.com/cmdoret/sshwarm/releases/tag/v${version}";
    license = lib.licenses.gpl3Plus;
    platforms = lib.platforms.unix;
    mainProgram = "sshwarm";
  };
}
