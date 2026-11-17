# Release engine: `nerv-release version next`

This page documents the configuration (`<repo>/.ai-workflow/release.yaml`)
behind `nerv-release version next`, the only working command today (see
`odd/tasks/initial-release.md`). `nerv-release build`, `deploy` and `setup`
are not implemented yet.

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
