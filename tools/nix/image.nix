# A container image with only sshwarm, ssh, and its user.
{
  dockerTools,
  runCommand,
  nixpkgs,
  sshwarmFor,
  goarch,
}:
let
  # system for the image's machine.
  system =
    {
      amd64 = "x86_64-linux";
      arm64 = "aarch64-linux";
    }
    .${goarch};

  # ssh looks its user up, and finds ~/.ssh in that user's home: root's is /root.
  users = runCommand "sshwarm-users" { } ''
    mkdir -p $out/etc $out/root
    echo "root:x:0:0:root:/root:/sbin/nologin" > $out/etc/passwd
    echo "root:x:0:" > $out/etc/group
  '';

  sshwarm = sshwarmFor "linux" goarch;
in
dockerTools.streamLayeredImage {
  name = "ghcr.io/cmdoret/sshwarm";
  tag = goarch;
  architecture = goarch;
  # image build time -> overrides default 1970
  created = "now";
  contents = [
    sshwarm
    nixpkgs.legacyPackages.${system}.openssh
    users
  ];
  config = {
    Entrypoint = [ "/bin/sshwarm" ];
    # The standard labels: ghcr.io shows them, and links the package to its repository.
    Labels = {
      "org.opencontainers.image.source" = sshwarm.meta.homepage;
      "org.opencontainers.image.description" = sshwarm.meta.description;
      "org.opencontainers.image.licenses" = sshwarm.meta.license.spdxId;
      "org.opencontainers.image.version" = sshwarm.version;
      "org.opencontainers.image.revision" = sshwarm.rev;
    };
  };
}
