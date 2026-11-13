{
  description = "sshwarm: the state of a swarm of Linux machines, over ssh";

  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";

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
      packages = forAllSystems (system: {
        default = nixpkgs.legacyPackages.${system}.buildGoModule {
          pname = "sshwarm";
          version = "1.0.0";
          src = self;
          # After changing Go dependencies: nix build prints the new hash.
          vendorHash = "sha256-CgG2p1sMMluqwmwGB4eApyUFpvgDyRITYu1zV098wH0=";
          meta.mainProgram = "sshwarm";
        };
      });

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};

          baseTools = with pkgs; [
            go
            just
            shellcheck
            treefmt
            nixfmt
            shfmt
          ];
          devTools = with pkgs; [
            prek
          ];
        in
        {
          ci = pkgs.mkShell { packages = baseTools; };

          default = pkgs.mkShell {
            packages = baseTools ++ devTools;
            shellHook = "just setup";
          };
        }
      );
    };
}
