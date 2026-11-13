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
          vendorHash = builtins.readFile ./tools/nix/sshwarm-hash.txt;
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
            nixfmt
            nushell
            shellcheck
            shfmt
            treefmt
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
