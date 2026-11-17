# nerv-release

The release tool of the NERV ecosystem, for [Claude Code](https://claude.com/claude-code):
version computation, builds and deploys, independent of
[nerv](https://github.com/war-apps/nerv-by-gentle-ai). It installs and works
on a machine with no nerv at all, Windows included.

This repository is under initial development (see `odd/tasks/initial-release.md`).
Only `nerv-release version next` works today; the rest of the CLI (`build`,
`deploy`, `setup`, `install`, `configure`, ...) is not implemented yet.

## Build

```sh
go build ./cmd/nerv-release
```

## Test

```sh
go test ./...
```
