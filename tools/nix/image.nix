# An image for a Linux machine: sshwarm, the ssh it runs, and its user. The other
# machine's ssh comes from the binary cache: no emulation needed.
{
  dockerTools,
  runCommand,
  nixpkgs,
  sshwarmFor,
  goarch,
}:
let
  # The Nix system of the image's machine.
  system =
    {
      amd64 = "x86_64-linux";
      arm64 = "aarch64-linux";
    }
    .${goarch};

  # ssh looks its user up, and finds ~/.ssh in that user's home: root's is /root,
  # where users mount theirs (dockerTools.fakeNss's is /var/empty).
  users = runCommand "sshwarm-users" { } ''
    mkdir -p $out/etc $out/root
    echo "root:x:0:0:root:/root:/sbin/nologin" > $out/etc/passwd
    echo "root:x:0:" > $out/etc/group
  '';
in
dockerTools.streamLayeredImage {
  name = "ghcr.io/cmdoret/sshwarm";
  tag = goarch;
  architecture = goarch;
  contents = [
    (sshwarmFor "linux" goarch)
    nixpkgs.legacyPackages.${system}.openssh
    users
  ];
  config.Entrypoint = [ "/bin/sshwarm" ];
}
