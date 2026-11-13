{
  description = "sshwarm: the state of a swarm of Linux machines, over ssh";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

  # The outputs: their recipes are in ./tools/nix.
  outputs =
    { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          # The binary for the current system
          sshwarm = pkgs.callPackage ./tools/nix/sshwarm.nix { src = self; };

          # The binary for another OS and architecture
          sshwarmFor = goos: goarch: pkgs.callPackage ./tools/nix/cross.nix { inherit sshwarm goos goarch; };
          # The docker image for a Linux architecture
          imageFor = goarch: pkgs.callPackage ./tools/nix/image.nix { inherit nixpkgs sshwarmFor goarch; };
        in
        {
          default = sshwarm;
          # A directory of the release's binaries: sshwarm-linux-amd64, ...
          release = pkgs.callPackage ./tools/nix/release.nix { inherit sshwarmFor; };
        }
        // nixpkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          image-amd64 = imageFor "amd64";
          image-arm64 = imageFor "arm64";
        }
      );

      devShells = forAllSystems (
        system: import ./tools/nix/shells.nix { pkgs = nixpkgs.legacyPackages.${system}; }
      );
    };
}
