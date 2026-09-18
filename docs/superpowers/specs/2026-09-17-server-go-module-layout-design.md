# Server-Owned Go Module Layout Design

## Context

The repository currently has one Go module at the repository root. Its Go
packages are split across `server/`, `app/`, and one Docker runtime smoke-test
package under `deploy/docker/`.

Repository-wide import analysis shows that the six packages under `app/` are
used only by Server code:

- `config`
- `converter`
- `draft`
- `image`
- `wechat`
- `writer`

The TypeScript Agent runtime, Studio, and Harness do not import these
Go packages. The Docker runtime smoke test imports Server configuration only
to validate the generated Server configuration, so it belongs to the Server
test boundary as well.

## Goals

- Make `server/` the complete and only Go module in this repository.
- Move the former shared Go library to `server/app/*` without changing package
  behavior or public Go identifiers.
- Keep existing Server package import paths stable wherever possible.
- Keep all Server tests, including the Docker runtime smoke tests, runnable
  from the Server module.
- Preserve root-level developer commands through the existing Makefile.

## Non-Goals

- Do not merge the `app` packages into `server/service`, `server/config`, or
  other domain packages.
- Do not redesign configuration, image generation, conversion, publishing, or
  writer behavior.
- Do not preserve the old `github.com/anbanai/anban-creator/app/*` import paths.
  No repository-owned consumer uses them outside Server.
- Do not create a root `go.work` or a second Go module for deployment tests.
- Do not modify unrelated in-progress Server lifecycle work already present in
  the working tree.

## Target Layout

```text
anbanwriter/
├── Makefile
├── deploy/
│   └── docker/
├── server/
│   ├── go.mod
│   ├── go.sum
│   ├── app/
│   │   ├── config/
│   │   ├── converter/
│   │   ├── draft/
│   │   ├── image/
│   │   ├── wechat/
│   │   └── writer/
│   ├── runtime_smoke_test.go
│   └── ... existing Server packages
├── agent-ts/
├── harness/
└── studio/
```

The repository root will no longer contain `app/`, `go.mod`, or `go.sum`.

## Module And Import Paths

`server/go.mod` will declare:

```go
module github.com/anbanai/anban-creator/server
```

Existing Server imports such as
`github.com/anbanai/anban-creator/server/service` remain unchanged because the
module root moves down by the same `server` path segment added to the module
name.

Former App imports change mechanically:

```text
github.com/anbanai/anban-creator/app/config
    -> github.com/anbanai/anban-creator/server/app/config
```

The same mapping applies to `converter`, `draft`, `image`, `wechat`, and
`writer`. Imports between the moved packages use the new Server-owned paths.

The dependency versions in `go.mod` remain unchanged during the move. After
all packages compile from the new module root, `go mod tidy` runs in `server/`
to remove dependencies that are no longer reachable and update `server/go.sum`
deterministically.

## Docker Runtime Smoke Tests

`deploy/docker/runtime-smoke_test.go` moves to
`server/runtime_smoke_test.go` and joins package `main`. Its repository-root
resolver changes to walk one directory upward from the test file. The test
continues reading the canonical scripts and Dockerfiles from `deploy/docker/`;
the deployment assets themselves do not move.

This keeps the test inside the only Go module while retaining its current
behavior and coverage.

## Build And Tooling Changes

Root Make targets remain the supported developer interface. Go commands in the
Makefile execute against `server/`, including tests, formatting, vetting,
coverage, Agent Pack generation, and binary builds. Server binaries continue
to be written to the repository-level `bin/` directory.

GitHub Actions will use `server/go.sum` as the Go dependency cache key and run
Go download, build, vet, test, and release commands from the Server module.

`deploy/docker/Dockerfile.server` will copy `server/go.mod` and
`server/go.sum` before source code, then build from `/build/server`. The final
image paths and runtime command remain unchanged.

Repository documentation will use `cd server && go ...` for direct Go commands
and describe `server/app/` as Server-owned reusable packages. References to the
removed root `app/` layout will be eliminated from active docs and source
comments.

## Compatibility And Failure Handling

This is a source-layout breaking change for hypothetical external consumers of
the old App import paths. The repository does not provide a compatibility shim
because those packages are not a supported standalone product surface and the
project explicitly does not preserve obsolete compatibility paths.

The move is behavior-preserving. Compile failures are expected to identify any
missed import or module-relative path. Contract tests will also assert that:

- root `app/`, `go.mod`, and `go.sum` are absent;
- `server/app/`, `server/go.mod`, and `server/go.sum` exist;
- the Server module path is `github.com/anbanai/anban-creator/server`;
- no Go source imports `github.com/anbanai/anban-creator/app/*`;
- Server Docker and repository build commands target the new module layout.

## Verification

The completed migration must pass fresh runs of:

```bash
cd server && go test ./...
cd server && go vet ./...
cd server && go build -o /tmp/anban-creator-server .
make agent-pack-check
make docker-runtime-smoke
```

Structural searches must find no active reference to the old root App package
paths or root Go module files. Existing unrelated working-tree changes must
remain intact.
