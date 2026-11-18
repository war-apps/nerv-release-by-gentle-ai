# Release engine: `nerv-release version next` and `nerv-release build`

This page documents the configuration (`<repo>/.ai-workflow/release.yaml`)
behind `nerv-release version next`, `nerv-release build` and `nerv-release
setup --import` (see `odd/tasks/initial-release.md`). `nerv-release deploy`
is not implemented yet.

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
(`release.version`, by default `conventional-commits`, today's native
engine) and the **build** (`release.build` for every project, overridden
by `release.projects[].build`: `bash`, or `docker-compose` once ported).
A dependency's bump defaults to `patch`; a stage command without a shell
defaults to `bash`.

See `odd/tasks/initial-release.md` for the full configuration shape
(stages, hooks, coverage, version files, images) as it is ported from
nerv.

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
