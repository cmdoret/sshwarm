# Contributing

## Setup

With [Nix](https://nixos.org/download) and [direnv](https://direnv.net):

```sh
git clone https://github.com/cmdoret/sshwarm && cd sshwarm
direnv allow   # or: nix develop
```

The dev shell has Go and all the tooling to work in the repo.

## Tasks

| | |
|---|---|
| `just test` | tests |
| `just lint` | golangci-lint, shellcheck, `nix flake check` |
| `just format` | format everything |
| `just build` | binary and Nix package, into `.output/` |
| `just update` | Go dependencies, flake lock, vendor hash |
| `just docs::serve` | this documentation, live |
| `just` | every recipe |

The git hooks format, lint, and update the Nix vendor hash when `go.mod` changes:
the commit then fails; stage the new hash and commit again.

## Tests

- No real ssh: `testutil.FakeBin` puts a fake `ssh` first on `PATH`.
- The probe fixtures in `internal/swarm/testdata` come from real machines:
  `sshwarm probe <machine>`.
- The browser's tests type keys and read the screen: `internal/ui/tui_test.go`.

## Code

See [architecture](architecture.md).
Style: [Google's Go style guide](https://google.github.io/styleguide/go/).
Comments say why, not what.

## Release

```sh
just release::bump minor   # or patch, major, x.y.z: commits the version, tags it
git push --follow-tags
```

Then publish a GitHub release of the tag. CI uploads the binaries and pushes the
images: `x.y.z`, and `latest` unless it's a pre-release.
