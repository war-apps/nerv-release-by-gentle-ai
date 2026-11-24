# nerv-release

<p align="center">
  <a href="https://code.claude.com/docs"><img alt="Claude Code plugin" src="https://img.shields.io/badge/Claude_Code-plugin-d97757?style=for-the-badge&logo=anthropic&logoColor=white"></a>
  <a href="https://go.dev/"><img alt="Go CLI and engine" src="https://img.shields.io/badge/Go-CLI_%26_engine-00ADD8?style=for-the-badge&logo=go&logoColor=white"></a>
  <a href="https://www.markdownguide.org/"><img alt="Markdown commands and agents" src="https://img.shields.io/badge/Markdown-commands_%26_agents-000000?style=for-the-badge&logo=markdown&logoColor=white"></a>
  <a href="https://yaml.org/"><img alt="YAML config" src="https://img.shields.io/badge/YAML-release.yaml-CB171E?style=for-the-badge&logo=yaml&logoColor=white"></a>
  <a href="https://www.conventionalcommits.org/"><img alt="Conventional Commits" src="https://img.shields.io/badge/Conventional_Commits-versioning-FE5196?style=for-the-badge&logo=conventionalcommits&logoColor=white"></a>
  <a href="https://docs.docker.com/compose/"><img alt="Docker Compose" src="https://img.shields.io/badge/Docker_Compose-build_%26_deploy-2496ED?style=for-the-badge&logo=docker&logoColor=white"></a>
  <a href="https://docs.github.com/repositories/releasing-projects-on-github"><img alt="GitHub releases" src="https://img.shields.io/badge/GitHub-releases-181717?style=for-the-badge&logo=github&logoColor=white"></a>
  <a href="https://infisical.com/"><img alt="Infisical over SSH" src="https://img.shields.io/badge/Infisical-SSH_deploy-000000?style=for-the-badge"></a>
  <a href="https://github.com/war-apps/nerv-by-gentle-ai"><img alt="NERV ecosystem" src="https://img.shields.io/badge/NERV-ecosystem_tool-8a2be2?style=for-the-badge"></a>
  <a href="https://github.com/war-apps/nerv-kit-by-gentle-ai"><img alt="nerv-kit" src="https://img.shields.io/badge/nerv--kit-shared_library-5a4fcf?style=for-the-badge"></a>
  <a href="https://www.gnu.org/software/bash/"><img alt="Bash and PowerShell installers" src="https://img.shields.io/badge/install-sh_%7C_ps1-4EAA25?style=for-the-badge&logo=gnubash&logoColor=white"></a>
  <a href="https://goreleaser.com/"><img alt="GoReleaser" src="https://img.shields.io/badge/GoReleaser-releases-0F9D9A?style=for-the-badge"></a>
  <a href="https://git-scm.com/"><img alt="git" src="https://img.shields.io/badge/git-gitflow-F05032?style=for-the-badge&logo=git&logoColor=white"></a>
</p>

<p align="center">
  <a href="https://github.com/Gentleman-Programming/gentle-ai"><img alt="Built with Gentle-AI" src="https://raw.githubusercontent.com/Gentleman-Programming/gentle-ai/main/docs/assets/brand/built-with-gentle-ai.png"></a>
</p>

The release tool of the NERV ecosystem, for [Claude Code](https://claude.com/claude-code):
version computation, builds and deploys, independent of
[nerv](https://github.com/war-apps/nerv-by-gentle-ai). It installs and works
on a machine with no nerv at all, Windows included.

`nerv-release` carries nerv's own `release` plugin, its 6 providers and the
Go release engine out as a standalone tool, so a repository can compute
its next version, build, test, package and deploy it without pulling in
the rest of the NERV workflow.

## Requirements

- [Claude Code](https://claude.com/claude-code): the `claude` CLI on
  `PATH` for `nerv-release install` (it registers the plugin
  marketplace through it).
- `git`, always; `docker` when a build saves images or a deploy
  publishes them.
- Per provider: `gh` with a `gh auth login` session (`release-github`),
  `bash` and `git` (`release-script`), `docker` and `ssh`
  (`release-ssh-compose`), plus `sshpass` for `ssh_auth: password` and
  the `infisical` CLI for `source: infisical`. A deploy's preflight
  refuses a missing tool before anything is pushed.
- Go 1.26 only to build from source.

Release binaries cover Linux, macOS and Windows on amd64 and arm64.

## Install

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/war-apps/nerv-release-by-gentle-ai/main/scripts/install.sh | sh
```

Downloads the right release archive, verifies its `checksums.txt` entry,
installs the binary to `$HOME/.local/bin` (override with
`NERV_RELEASE_INSTALL_DIR`), and then runs `nerv-release install`. Set
`NERV_RELEASE_VERSION` to pin an exact tag, or `NERV_RELEASE_NO_INSTALL`
to skip the post-install step. Piped from `curl`, the script has no
terminal, so `nerv-release install` runs with `--no-configure`; set
`NERV_RELEASE_TTY_DEVICE=/dev/tty` to reattach a terminal and get its
interactive prompts.

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/war-apps/nerv-release-by-gentle-ai/main/scripts/install.ps1 | iex
```

Same steps, installing to `%LOCALAPPDATA%\nerv-release\bin` by default
(override with `NERV_RELEASE_INSTALL_DIR`) and adding it to the user
`PATH`. `NERV_RELEASE_VERSION` and `NERV_RELEASE_NO_INSTALL` work the
same way as the shell installer.

`nerv-release install` materializes the `nerv-release` plugin
marketplace at `~/.nerv-release/marketplace` (core `release` plugin plus
its 6 provider plugins: `release-bash`, `release-conventional-commits`,
`release-docker-compose`, `release-github`, `release-script`,
`release-ssh-compose`) and registers it with Claude Code. Every provider
that was already enabled as `<name>@nerv` is carried over, every
provider already enabled as `<name>@nerv-release` is refreshed, and on a
machine with neither (a fresh install) every provider this build bundles
is installed. When nerv's own `release@nerv` or one of its providers is
enabled, it is disabled only after a successful install, and no other
nerv plugin is ever touched. On a terminal it then offers the
`nerv-release configure` wizard (`--no-configure` skips the offer).

### Upgrade

```sh
nerv-release upgrade          # newest stable release
nerv-release upgrade --alpha  # newest alpha pre-release
nerv-release upgrade --rc     # newest release candidate
```

Replaces the running binary and re-runs `nerv-release install`, so the
plugin bundle and its Claude Code registration follow the binary.

### Uninstall

There is no uninstall command; remove the pieces by hand:

```sh
claude plugin uninstall release@nerv-release   # and each <provider>@nerv-release
claude plugin marketplace remove nerv-release
rm -rf ~/.nerv-release                         # the materialized marketplace
rm "$(command -v nerv-release)"                # the binary
```

Optionally delete the global `release.yaml` (see
[Configuration](#configuration)). Repository files
(`.ai-workflow/release.yaml`, `.nerv/`) are yours to keep or remove.

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

Key flags of the engine commands (each also takes `--cwd <dir>`):

| Command | Flags |
|---|---|
| `version next` | `--branch`, `--project`, `--offline` (local tags and notes only), `--format text\|json\|env` |
| `build` | `--dry-run` (run the pipeline, write no artifact), `--expect <project>=<version>` (repeatable; refuse any other version), `--yes` (no confirmation), `--version <semver>` (override the computed one), `--force` (replace an existing artifact), `--branch`, `--project`, `--json` |
| `deploy` | `--artifact <dir>` (required), `--dry-run` (prepare and show the steps, push nothing), `--rebase` (re-apply on a moved origin branch), `--target <name>` (repeatable), `--yes`, `--json` |

A typical flow rehearses first, then runs exactly what the rehearsal
showed:

```sh
nerv-release version next
nerv-release build --dry-run
nerv-release build --expect api=1.4.0 --yes
nerv-release deploy --artifact .nerv/artifacts/api/1.4.0 --dry-run
nerv-release deploy --artifact .nerv/artifacts/api/1.4.0 --yes
```

### Scripting and CI

Progress goes to stderr and the result to stdout. The engine commands
share their exit codes: `0` success (also nothing to release, and a dry
run), `1` refused (invalid configuration, a local branch not equal to
origin, origin moved, a missing tool, no confirmation; nothing was
written), `2` a step, the environment or the usage failed, `128 + the
signal` when interrupted (130 Ctrl-C, 143 SIGTERM).

`version next --format env` prints GitHub Actions output lines
(`key=value`, multi-line values as `key<<DELIMITER` blocks); with several
projects and no `--project`, each key is prefixed with the project name
(`api_new_release_version`):

```yaml
- id: version
  run: nerv-release version next --format env >> "$GITHUB_OUTPUT"
- if: steps.version.outputs.new_release_published == 'true'
  run: echo "Releasing ${{ steps.version.outputs.new_release_version }}"
```

## Claude Code plugin

The `release` plugin wraps the CLI in slash commands. Every command that
writes rehearses with a dry run first and runs the real build or deploy
only after the user's explicit yes to exactly what the dry run printed.

| Command | What it does |
|---|---|
| `/release:version` | Shows the next version of the repository or of each monorepo project. |
| `/release:build` | Builds, tests and packages origin's code into a local artifact; never writes to git. |
| `/release:deploy` | Releases and deploys the latest local artifact or the one named: release commit, tag, push, targets and git host release. |
| `/release:ship` | Version, build and deploy in one go, each real run after its own yes; the deploy is skipped without a deploy provider. |
| `/release:setup` | Sets up the `release:` section interactively: imports a semantic-release config, detects the projects, asks each build provider, validates with `version next`. |
| `/release:deploy-setup` | Sets up the `deploy:` section interactively: the git host, the targets with their provider config, and git. |

The commands delegate to three agents: `versioner` (read-only; computes
and explains the next version), `builder` (dry run, then the real build
bound with `--expect`) and `deployer` (dry run, then the real deploy,
reporting each step). Agents never ask the user anything; a question goes
back to the session. Two skills hold the contracts they load: `release`
(the `release:` section, the version engine, the artifact, migration)
and `deploy` (the `deploy:` section, provider discovery and operations,
the deploy steps and re-runs).

### Providers

| Slot | Provider | What it does |
|---|---|---|
| version | [`conventional-commits`](plugin/extensions/release-conventional-commits/README.md) (default) | semantic-release's `conventionalcommits` behaviour, natively in Go. |
| build | [`bash`](plugin/extensions/release-bash/README.md) | Each stage runs the project's own command (bash, pwsh or node); built images are saved into the artifact. |
| build | [`docker-compose`](plugin/extensions/release-docker-compose/README.md) | `docker compose build`, a test service's exit code as the test result, images saved into the artifact. |
| host | [`github`](plugin/extensions/release-github/README.md) | Creates the GitHub release of the tag with `gh` (a prerelease on a channel). |
| target | [`script`](plugin/extensions/release-script/README.md) | Runs the repository's own publish, deploy and verify scripts with `bash`. |
| target | [`ssh-compose`](plugin/extensions/release-ssh-compose/README.md) | Updates a docker compose container over SSH, with deploy data from environment variables or Infisical, then checks its health and version label. |

## Configuration

`nerv-release` reads two YAML files, each holding the same `release:`
and `deploy:` sections:

- **Repository** — `.ai-workflow/release.yaml` at the repository root.
  Project values win over the global file, except personal (user-scope)
  provider keys, which only the global file may set.
- **Global** — `ai-workflow/release.yaml` in the user configuration
  directory: `$XDG_CONFIG_HOME/ai-workflow/release.yaml` on Linux
  (`~/.config` when unset), `~/Library/Application Support/ai-workflow/release.yaml`
  on macOS, `%AppData%\ai-workflow\release.yaml` on Windows
  (`nerv-release configure --print` shows the path). Holds a `defaults:`
  block (the seed `nerv-release init` and the setup commands use) and
  every provider key a manifest marks `scope: user`: today
  `release-ssh-compose`'s `ssh_auth`, `ssh_key_file`, `ssh_known_hosts`,
  `ssh_connect_timeout` and `infisical_domain`. Every other key is
  repository scope.

A minimal repository file:

```yaml
release:
  version: conventional-commits       # version engine (the default)
  build: bash                         # build provider of every project
  providers:
    conventional-commits:
      tag_format: "v${version}"
  projects:
    - name: api
      path: .
      version_files: [package.json]   # files the release writes its version into
      stages:                         # restore, lint, build, test, coverage, package
        test: { run: npm test }
        package: { run: 'npm pack --pack-destination "$NERV_RELEASE_OUTPUTS_DIR"' }

deploy:
  host: github                        # git host release (release-github)
  publish: true                       # push the artifact's images (the default)
  targets:                            # optional, deployed in order
    - name: staging
      provider: ssh-compose
      config: { environment: staging }
  git:
    remote: origin                    # the default
    commit_message: "chore(release): ${version} [skip ci]\n\n${notes}"
```

Both files are edited comment-preserving: `nerv-release configure`
and `nerv-release init` only ever touch the keys they change, keeping
everything else — comments, key order, formatting — byte for byte.

Each project's `version_files` lists the files the release writes its
version into, for any ecosystem: `package.json`, `package-lock.json`,
`composer.json`, `Cargo.toml`, `pyproject.toml`, `Chart.yaml`,
`pubspec.yaml`, `pom.xml`, `gradle.properties`, `VERSION` and MSBuild
project files by name, any other file through a format with keys or a
regex, and a `{command, files}` escape hatch. Only the old value's bytes
change.

The configuration reference — the provider slots, version files, the
stage environment variables and the `deploy:` section, with links to
every key — is [docs/release-commands.md](docs/release-commands.md).

## Build and deploy

`nerv-release build` clones origin's branch into a temporary directory,
writes the version files and the CHANGELOG section there, runs each
project's pipeline and prepare hooks, and leaves one artifact per
project:

```
.nerv/artifacts/<project>/<version>/
  manifest.json          project, version, channel, tag, base commit, images,
                         and the sha256 of every file
  notes.md               the release notes
  CHANGELOG.section.md   the CHANGELOG section
  outputs/               the packaged outputs (image tars, packages, directories)
  release-files/         the new contents of the files the release changes
```

`.nerv/` ignores itself, so your `.gitignore` is never changed. Build
only builds what origin has: a local branch ahead of or behind origin is
refused, and an existing artifact of the same version is kept unless
`--force`.

`nerv-release deploy --artifact <dir>` verifies the artifact's digests,
clones the remote at the artifact's base commit and:

1. commits the release files with `deploy.git.commit_message`;
2. creates the tag and semantic-release's channel note;
3. pushes the branch, the tag and the note atomically (never forced);
4. publishes the artifact's images (`docker load`, then `docker push`),
   unless there are none or `deploy.publish: false`;
5. creates the git host's release, unless it exists;
6. runs each target's publish, deploy and verify, in `deploy.targets`
   order.

Your repository is never modified. When origin's branch moved past the
artifact's base commit the deploy is refused, unless `--rebase`
re-applies the release files on the new head (the tagged code was then
never built; prefer releasing again). A re-run is idempotent: what
origin already has is skipped, so after a failure fix the cause and run
the same command again.

## Migrating from nerv or semantic-release

- A 4.2.0-shaped `release:` section (`semantic_release:`, project
  presets) and the old `pipeline:` section are refused, naming each key;
  convert them with nerv's `nerv migrate`, or by hand as the `release`
  skill's [migration section](plugin/release/skills/release/SKILL.md#6-migration)
  describes.
- `nerv-release init --import` copies nerv's `release:`/`deploy:` blocks
  from `.ai-workflow/config.yaml` into `.ai-workflow/release.yaml`,
  repository-scope keys only; user-scope keys are reported as skipped,
  for you to set with `nerv-release configure`.
- `nerv-release setup --import` (or `/release:setup`) translates a
  semantic-release config (`package.json` `release`, `.releaserc.json`,
  `.releaserc.yaml`) once and lists what it cannot translate; a `.js`
  config is reported, never imported.

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

## License

[CC0 1.0 Universal](LICENSE).

## Links

- [CHANGELOG.md](CHANGELOG.md)
- [docs/release-commands.md](docs/release-commands.md): configuration reference
- Providers: [release-conventional-commits](plugin/extensions/release-conventional-commits/README.md),
  [release-bash](plugin/extensions/release-bash/README.md),
  [release-docker-compose](plugin/extensions/release-docker-compose/README.md),
  [release-github](plugin/extensions/release-github/README.md),
  [release-script](plugin/extensions/release-script/README.md),
  [release-ssh-compose](plugin/extensions/release-ssh-compose/README.md)
- [nerv](https://github.com/war-apps/nerv-by-gentle-ai): the NERV workflow
- [nerv-kit](https://github.com/war-apps/nerv-kit-by-gentle-ai): the shared library
