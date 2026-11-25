# Release configuration reference

This page documents the configuration (`<repo>/.ai-workflow/release.yaml`)
behind `nerv-release version next`, `nerv-release build`,
`nerv-release deploy`, `nerv-release setup --import` and
`nerv-release migrate`: the `release:`
section (version engine, build providers, projects, version files, stage
environment) and the `deploy:` section (git host, targets, git).

The version engine is native: it gives the version, the tag, the release
notes and the CHANGELOG section that semantic-release with the
`conventionalcommits` preset gives, without semantic-release, node or npm.

A repository without a `release:` section is one project with the
default branches. A monorepo declares one project per releasable unit:

```yaml
release:
  version: conventional-commits
  projects:
    - name: api
      path: src/api
      build: bash
```

The `release:` section has two provider slots: the **version engine**
(`release.version`, by default `conventional-commits`, the native
engine) and the **build** (`release.build` for every project, overridden
by `release.projects[].build`: `bash` or `docker-compose`). A provider's
keys live under `release.providers.<provider>`, and a project overrides a
build provider's keys under `release.projects[].providers.<provider>`.
A dependency's bump defaults to `patch`; a stage command without a shell
defaults to `bash`. Unknown keys are refused, with a path-qualified
message per problem.

The full key-by-key shape (branches, release rules, tag format,
changelog, stages, hooks, coverage, images) is the `release` skill's
[configuration section](../plugin/release/skills/release/SKILL.md#2-configuration);
each provider's keys are in its README under
[plugin/extensions/](../plugin/extensions/).

## Version files

`release.projects[].version_files` lists the files `nerv-release build`
writes the new version into, in the release clone, in order, before the
first stage. Each file's new content is captured once into the
artifact's `release-files/`, for deploy's release commit. Paths are
relative to the repository root, with forward slashes.

An entry is a path, whose format is inferred from the file name, or a
mapping `{path, key | keys, pattern, format, style}`:

```yaml
release:
  projects:
    - name: api
      path: .
      build: bash
      version_files:
        - src/Api/Api.csproj                      # msbuild
        - package.json                            # json: version
        - package-lock.json                       # json: version, packages[""].version
        - { path: deploy/values.yaml, format: yaml, key: image.tag }
        - { path: src/app/__init__.py, format: regex, pattern: '__version__ = "(?P<version>[^"]+)"', style: pep440 }
        - command: npm version {version} --no-git-tag-version --allow-same-version
          files: [package.json, package-lock.json]
```

| Format | Inferred for | Default keys |
|---|---|---|
| `msbuild` | `*.csproj`, `*.fsproj`, `*.vbproj`, `*.props`, `*.targets` | `<Version>`, inserted when missing; `<PackageReleaseNotes>` gets the release link when origin has a public address |
| `json` | `package.json`, `composer.json` / `package-lock.json` | `version` / `version` and `packages[""].version` |
| `toml` | `Cargo.toml` / `pyproject.toml` | `package.version` / the first of `project.version`, `tool.poetry.version` the file has |
| `yaml` | `Chart.yaml` / `pubspec.yaml` | `version` and `appVersion` / `version` |
| `xml` | `pom.xml` | `project/version` |
| `properties` | `gradle.properties` | `version` |
| `text` | `VERSION`, `VERSION.txt` | the whole file, which holds only the version (trailing line breaks kept) |
| `regex` | never inferred | `pattern`, with a named group `(?P<version>...)`: every match's group is replaced |

- **Keys.** `key` is one key and `keys` several (not both). json, toml
  and yaml keys are dotted paths, a segment bracket-quoted when it has a
  dot or is empty (`packages[""].version`, `["a.b"].version`); an xml key
  is a slash path of element names (`project/version`); a properties key
  is the property name (`app.version`). msbuild, text and regex take no
  keys. A file the table does not name needs `format`, plus `keys` for
  json, toml, yaml, xml and properties, or `pattern` for regex.
- **Edits.** Only the old value's bytes are replaced: BOM, line endings,
  comments, indentation and key order stay byte for byte. A missing key,
  or a pattern with no match, fails the build; only msbuild inserts.
- **`style: pep440`** writes the version the Python way: `1.2.0-alpha.3`
  → `1.2.0a3`, `beta.N` → `bN`, `rc.N` → `rcN`; a release is unchanged.
  Any other prerelease identifier, or build metadata, fails the build.
- **`{command, files, style}`** is the escape hatch: the command runs in
  the release clone through `bash`, with `{version}` replaced by the
  version (in `style`) and the variables below, and each of `files` is
  captured after it ran. It takes no `key`, `keys`, `pattern` or `format`.
- **Paths.** Every entry is validated when the section loads: an
  unknown format, keys a format does not take, a pattern without its
  group, a path with a backslash, an absolute path, or one that leaves
  the repository (`..`) is a configuration error. At build time a
  version file, a command's file or the CHANGELOG reached through a
  symlink or a junction, or that is not a regular file, is refused, so a
  committed symlink can never make the release write outside the clone.

`nerv-release setup --import` adds the files semantic-release writes:
`@semantic-release/npm`'s `package.json` (under `pkgRoot`, with
`package-lock.json` when it exists) and each `@semantic-release/git` asset
that is a plain path in a known format, to the only project's
`version_files`; with no project or several it prints them in a warning.
Globs and other assets are warnings too; the changelog asset is the
`changelog` key's. `/release:setup` detects the known files of each
project and proposes them.

## Migrate

`nerv-release migrate [--cwd <dir>] [--user] [--dry-run]` converts the
legacy sections nerv moved into `release.yaml`, as nerv 6.6's `nerv
migrate` converted them in nerv's own file:

| Legacy | Becomes |
|---|---|
| `ship:` (5.0.0-alpha.1) | `release:`, its keys moved as below; refused next to a `release:` section |
| engine keys at the top of `release:`, a project's `build:` mapping | `release.providers.conventional-commits.<key>`; `build: docker-compose` or `bash`, `file`/`test_service`/`shared_host` under the project's `providers.<provider>`, `images`/`version_files`/`coverage` on the project |
| 4.2.0 `release:` (`semantic_release:`, project presets) | each project `build: bash` with the commands its presets ran (its own stages win), `version_files` and `coverage` from the dotnet preset, `images` from the docker preset; `semantic_release:`, the publish stage and hooks dropped; the semantic-release configuration imported into `release.providers.conventional-commits` |
| 4.2.0 `pipeline:` | `deploy:` with one `release-ssh-compose` target per `pipeline.deploy` environment (shared settings in each), and `host: github` when origin is GitHub; an existing `deploy:` is kept |
| `provider: bash` deploy targets | `provider: script` (also in the global file's `deploy:` and `defaults.deploy` with `--user`) |
| `.ai-workflow/providers/deploy-<name>` | `release-<name>`, `extension: release` |

What has no equivalent (npm and NuGet pushes, `pipeline.sync`,
`selection: branches`, ...) is printed under "Check by hand", and the
file gets a `TODO(nerv-release migrate):` comment where it applies. The
personal `release-ssh-compose` keys of a `pipeline:` (`ssh_auth`,
`ssh_key_file`, `ssh_known_hosts`, `ssh_connect_timeout`,
`infisical_domain`) move to the global `release.yaml`
(`deploy.providers.ssh-compose.<key>`) with `--user`, never overwriting
a value it already has; without it they are reported and left out.

The result must load with today's loaders, or nothing is written.
Every replaced file is backed up next to it
(`<file>.bak-migrate-<timestamp>`); `--dry-run` prints the plan and
writes nothing; a second run prints `nothing to migrate`. A symlinked
`release.yaml` or `.ai-workflow` folder is refused. Exit codes: 0
migrated, nothing to migrate or a dry run; 1 refused; 2 usage or
environment error.

## Environment variables

`nerv-release build` runs each stage's and hook's command (and script
file) with the inherited environment, the command's own `env`, and these
`NERV_RELEASE_*` variables (empty when the value does not apply):

| Variable | Meaning |
|---|---|
| `NERV_RELEASE_PROJECT` | Project name. |
| `NERV_RELEASE_PROJECT_PATH` | Project `path`, repository-relative with forward slashes (`.` for the root). |
| `NERV_RELEASE_REPO_ROOT` | Absolute repository root (the release clone). |
| `NERV_RELEASE_STAGE` | The stage or hook key: `test`, `before_test`, `prepare`, ... |
| `NERV_RELEASE_VERSION` | Next version, e.g. `1.2.0-alpha.2`. |
| `NERV_RELEASE_CHANNEL` | Release channel (`alpha`, `rc`, ...); empty for the default channel. |
| `NERV_RELEASE_TAG` | Next tag, e.g. `api-v1.2.0`. |
| `NERV_RELEASE_GIT_HEAD` | The origin commit being built. |
| `NERV_RELEASE_LAST_VERSION` | Last release on the channel; empty when none. |
| `NERV_RELEASE_NOTES_FILE` | File holding the release notes (Markdown). |
| `NERV_RELEASE_BRANCH` | Branch being built. |
| `NERV_RELEASE_DRY_RUN` | `true` under `nerv-release build --dry-run`, else `false`. |
| `NERV_RELEASE_ARTIFACTS_DIR` | Per-project directory for build outputs, outside the repository. |
| `NERV_RELEASE_OUTPUTS_DIR` | `$NERV_RELEASE_ARTIFACTS_DIR/outputs`, emptied before the run: what the package stage writes there becomes the artifact's `outputs/`. |
| `NERV_RELEASE_COVERAGE_DIR` | With `coverage`, the directory the coverage check searches for `*.cobertura.xml` reports (`coverage.dir` in the clone, or `$NERV_RELEASE_ARTIFACTS_DIR/coverage`); not set otherwise. |

## Deploy section

`nerv-release deploy --artifact <dir>` reads the `deploy:` section:

```yaml
deploy:
  host: github                    # git host provider (release-github); empty creates no host release
  publish: true                   # push the artifact's images in the publish step (default)
  targets:                        # optional; deployed in this order
    - name: staging
      provider: ssh-compose       # target provider: ssh-compose or script
      config:
        environment: staging
  git:
    commit_message: "chore(release): ${nextRelease.version} [skip ci]

${nextRelease.notes}"
    remote: origin                # cloned and pushed to (default)
```

`git.commit_message` takes `${nextRelease.version}`, `${nextRelease.gitTag}`,
`${nextRelease.channel}`, `${nextRelease.notes}` and the short forms
`${version}`, `${tag}`, `${channel}`, `${notes}`. A target's provider key
comes from its `config`; when unset, from `deploy.providers.<provider>.<key>`
in the global `release.yaml`; otherwise from the provider's default. A
`scope: user` key (credentials, local paths: `release-ssh-compose`'s
`ssh_auth`, `ssh_key_file`, `ssh_known_hosts`, `ssh_connect_timeout`,
`infisical_domain`) belongs only in the global file, set with
`nerv-release configure`. The full contract is the `deploy` skill's
[configuration section](../plugin/release/skills/deploy/SKILL.md#2-configuration).
