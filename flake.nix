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
          version = "0.1.0";
          src = self;
          # After changing Go dependencies: nix build prints the new hash.
          vendorHash = "sha256-OAWTxe4SpQsQLJpz6y1QAiJq2hvNkwjAHP2Dj99z8qI=";
          meta.mainProgram = "sshwarm";
        };
      });

      devShells = forAllSystems (system: {
        default = nixpkgs.legacyPackages.${system}.mkShell {
          packages = with nixpkgs.legacyPackages.${system}; [
            go
            just
          ];
        };
      });
    };
}
