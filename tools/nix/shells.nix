# The dev shells: `ci` has what CI runs, `default` adds the git hooks, installed on
# entering.
{ pkgs }:
let
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
