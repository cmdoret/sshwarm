# Contributing

The following kind of contributions are welcome:

* New Issues (bug reports, questions, ideas, ...)
* Pull Requests (documentation improvements, code improvements, new features, ...)

## Setup

Setup [Nix](https://nixos.org/download) and [direnv](https://direnv.net) for your shell:

```sh
git clone https://github.com/cmdoret/sshwarm && cd sshwarm
direnv allow   # or: nix develop
```

The nix dev shell has Go and all the tooling to work in the repo.

## Tasks

We use just as a task runner:

| | |
|---|---|
| `just test` | tests |
| `just lint` | golangci-lint, shellcheck, `nix flake check` |
| `just format` | format everything |
| `just build` | binary and Nix package, into `.output/` |
| `just update` | Go dependencies, flake lock, vendor hash |
| `just docs::serve` | this documentation, live |
| `just` | show every recipe |

The git hooks format, lint, and update the Nix vendor hash when `go.mod` changes:
the commit then fails; stage the new hash and commit again.

## Tests

- No real ssh: `testutil.FakeBin` puts a fake `ssh` first on `PATH`.
- The probe fixtures in `internal/swarm/testdata` come from real machines:
  `sshwarm probe <machine>`.
- The browser's tests type keys and read the screen: `internal/ui/tui_test.go`.

## Code

See [architecture](/docs/architecture.md).
Style: [Google's Go style guide](https://google.github.io/styleguide/go/).
Try to keep comments concise, and say why, not what.

## Release

```sh
just release::bump minor   # or patch, major, x.y.z: commits the version, tags it
git push --follow-tags
```

Then publish a GitHub release of the tag. CI uploads the binaries and pushes the
images: `x.y.z`, and `latest` unless it's a pre-release.

## AI contributions

AI contributions are welcome, as long as:

* the code passes all checks and follows the quality standards in place.
* They are not unnecessarily verbose or overengineered
* The PR descriptions issues and discussions are humen written

The use of [samber/cc-skills-golang](https://github.com/samber/cc-skills-golang) is recommended.
