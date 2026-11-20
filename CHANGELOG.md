# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.1] - 2026-11-20

### Fixed

- The tests no longer read or write the developer's real user config:
  a test that faked only the home directory still reached
  `%AppData%\ai-workflow\release.yaml` on Windows (and
  `$XDG_CONFIG_HOME` elsewhere) through nerv-kit's user config
  directory, so `go test` left a test value in the real file. Every test
  binary that can reach that directory now points it at a temporary one.

## [1.0.0] - 2026-11-20

First release: nerv's release extension as an independent tool of the
NERV ecosystem. It installs with `scripts/install.sh` or
`scripts/install.ps1` and works without nerv, Windows included; on a
machine with nerv's own `release@nerv` plugin or its providers,
`nerv-release install` disables them after a successful install so
`/release:*` is not duplicated.

### Added

- `nerv-release version next`, `build`, `deploy` and `setup --import`:
  the version engine (conventional commits, channels per branch), builds
  that package what origin has into a local artifact without writing to
  git, and deploys that make the release commit, the tag, the git host
  release, the image publish and the targets.
- `nerv-release install`, `upgrade`, `configure`, `init` and `version`.
- The `nerv-release` plugin marketplace: the `release` plugin
  (`/release:*` commands and the `release` skill) and its six providers
  (`release-conventional-commits`, `release-bash`,
  `release-docker-compose`, `release-github`, `release-script`,
  `release-ssh-compose`).
- Generic `version_files`: an entry is a path, its format inferred from
  the file name (`package.json`, `package-lock.json`, `composer.json`,
  `Cargo.toml`, `pyproject.toml`, `Chart.yaml`, `pubspec.yaml`, `pom.xml`,
  `gradle.properties`, `VERSION`, `VERSION.txt`, MSBuild project files), or
  a mapping `{path, key | keys, pattern, format, style}` for the msbuild,
  json, toml, yaml, xml, properties, text and regex formats; `style: pep440`
  for Python versions; `{command, files}` runs a command and captures the
  files it changes. Edits replace only the old value's bytes. Entries are
  validated when the section loads; backslashes, absolute paths, `..`,
  symlinks and junctions are refused.
- `nerv-release setup --import` adds `@semantic-release/npm`'s
  `package.json` (and an existing `package-lock.json`) and the
  `@semantic-release/git` assets in a known format to the only project's
  `version_files`, or names them in a warning when there is no project or
  several; `/release:setup` detects version files and proposes them.
