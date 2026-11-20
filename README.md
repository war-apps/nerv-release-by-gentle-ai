# nerv-release

The release tool of the NERV ecosystem, for [Claude Code](https://claude.com/claude-code):
version computation, builds and deploys, independent of
[nerv](https://github.com/war-apps/nerv-by-gentle-ai). It installs and works
on a machine with no nerv at all, Windows included.

`nerv-release` carries nerv's own `release` plugin, its 6 providers and the
Go release engine out as a standalone tool, so a repository can compute
its next version, build, test, package and deploy it without pulling in
the rest of the NERV workflow. This repository is under initial
development (see `odd/tasks/initial-release.md`); the CLI below works
today.

## Install

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/war-apps/nerv-release-by-gentle-ai/main/scripts/install.sh | sh
```

Downloads the right release archive, verifies its `checksums.txt` entry,
installs the binary to `$HOME/.local/bin` (override with
`NERV_RELEASE_INSTALL_DIR`), and then runs `nerv-release install`. Set
`NERV_RELEASE_VERSION` to pin an exact tag, or `NERV_RELEASE_NO_INSTALL`
to skip the post-install step.

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/war-apps/nerv-release-by-gentle-ai/main/scripts/install.ps1 | iex
```

Same steps, installing to `%LOCALAPPDATA%\nerv-release\bin` by default
(override with `NERV_RELEASE_INSTALL_DIR`) and adding it to the user
`PATH`. `NERV_RELEASE_VERSION` and `NERV_RELEASE_NO_INSTALL` work the
same way as the shell installer.

`nerv-release install` materializes the `nerv-release` plugin
marketplace (core `release` plugin plus its 6 provider plugins:
`release-bash`, `release-conventional-commits`, `release-docker-compose`,
`release-github`, `release-script`, `release-ssh-compose`) and registers
it with Claude Code. Every provider that was already enabled as
`<name>@nerv` is carried over, every provider already enabled as
`<name>@nerv-release` is refreshed, and on a machine with neither (a
fresh install) every provider this build bundles is installed. When
nerv's own `release@nerv` or one of its providers is enabled, it is
disabled only after a successful install, and no other nerv plugin is
ever touched. `nerv-release upgrade [--alpha|--rc]` replaces the running
binary with the newest release and re-runs install.

## Commands

```
Engine commands:
  version next [flags]  Compute the next release version with the native
                         engine (see "nerv-release version next --help")
  build [flags]          Build, test and package origin's code into a local
                         artifact, no git writes (see "nerv-release build --help")
  deploy --artifact <dir> [flags]
                         Release and deploy a release artifact (see
                         "nerv-release deploy --help")
  setup --import [--force] [--dry-run]
                         Import a semantic-release configuration into the
                         release: section (see "nerv-release setup --help")

Tool commands:
  install [--no-configure]
                         Install nerv-release (see "nerv-release install --help")
  upgrade [--alpha | --rc]
                         Replace this binary with the newest release
  configure [--print] [--json] [--set k=v] [--answers f]
                         Configure nerv-release (see "nerv-release configure --help")
  init [--import] [--none]
                         Write this repository's release.yaml (see
                         "nerv-release init --help")
  version [--json]       Print the binary and plugin versions
```

- `nerv-release version next` computes the next semantic version of the
  repository (or of each monorepo project in `release:`) with the native
  engine configured by `release.providers.<engine>` — never
  semantic-release. It is read-only: nothing in the repository changes.
- `nerv-release build` runs the build pipeline (version, test, package)
  in a throwaway clone of `origin` and leaves the result under
  `.nerv/artifacts/<project>/<version>/`; it never writes to git either.
- `nerv-release deploy --artifact <dir>` takes a build artifact and
  performs the release commit, tag and push, publishes images, and runs
  every configured deploy target, as the `deploy:` section describes.
- `nerv-release setup --import` reads a `.releaserc`/`package.json`
  semantic-release configuration and proposes the matching `release:`
  section (and the version files it detects) without writing anything
  unless confirmed.
- `nerv-release configure` is the interactive wizard for every
  user-scope provider key (for example `release-ssh-compose`'s
  `ssh_auth`/`ssh_key_file`); on a non-interactive shell it prints the
  current configuration instead of prompting. `--print`/`--json` dump it
  non-interactively, `--set key=value` sets one key without prompting.
- `nerv-release init` writes this repository's own
  `.ai-workflow/release.yaml`, seeded from the global file's `defaults:`
  section; `--import` pulls `release:`/`deploy:` out of nerv's own
  `.ai-workflow/config.yaml` (repository-scope keys only — personal keys
  are never imported); `--none` writes nothing.
- `nerv-release version [--json]` reports `{"binary", "plugin"}`, the
  NERV ecosystem version contract every tool exposes so `nerv` can tell
  whether `nerv-release` itself is installed and up to date.

## Configuration

`nerv-release` reads two YAML files, each holding the same `release:`
and `deploy:` sections:

- **Repository** — `.ai-workflow/release.yaml` at the repository root.
  Project values win over the global file, except personal (user-scope)
  provider keys, which only the global file may set.
- **Global** — `~/.config/ai-workflow/release.yaml` on macOS/Linux,
  `%AppData%\ai-workflow\release.yaml` on Windows. Holds a `defaults:`
  block (the seed `nerv-release init` and the setup commands use) and
  every provider key a manifest marks `scope: user` (for example
  `release-ssh-compose`'s `ssh_auth`, `ssh_key_file`,
  `infisical_domain`, `ssh_known_hosts`, `ssh_connect_timeout`).

Both files are edited comment-preserving: `nerv-release configure`
and `nerv-release init` only ever touch the keys they change, keeping
everything else — comments, key order, formatting — byte for byte.

Build artifacts are written to `.nerv/artifacts/<project>/<version>/`
in the repository, regardless of which file configured the build.

Each project's `version_files` lists the files the release writes its
version into, for any ecosystem: `package.json`, `package-lock.json`,
`composer.json`, `Cargo.toml`, `pyproject.toml`, `Chart.yaml`,
`pubspec.yaml`, `pom.xml`, `gradle.properties`, `VERSION` and MSBuild
project files by name, any other file through a format with keys or a
regex, and a `{command, files}` escape hatch. Only the old value's bytes
change. See [docs/release-commands.md](docs/release-commands.md#version-files).

## Relation to nerv

`nerv-release` is the same release engine [nerv](https://github.com/war-apps/nerv-by-gentle-ai)
shipped as its `release` plugin and 6 providers, carried out as an
independent tool with its own CLI, releases and configuration files, so
it installs and works without nerv at all. `nerv-release install` takes
over from nerv automatically: it carries over any provider that was
enabled under nerv's own marketplace and disables nerv's copies only
after a successful install. Projects that use the rest of the NERV
workflow (tasks, orchestration, ...) keep using nerv for those; only
release/deploy moves here.

## Build

```sh
go build ./cmd/nerv-release
```

## Test

```sh
go test ./...
```
