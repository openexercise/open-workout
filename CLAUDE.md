# CLAUDE.md

Open Workout: an open format for a workout log, the MCP tool profile and its template server, the
API core, the conformance suite, and the Go and TypeScript clients (v0 draft until the first outside
app writes it in production).

version_file: clients/ts/package.json (TypeScript client; tag `clients-ts-vX.Y.Z` publishes to
npm). The Go client is tagged `clients/go/vX.Y.Z`.

## Rules

- Until v1 the schema, profile, API documents and clients are published here from Lifterclub, the
  reference implementation (`scripts/publish-openworkout.sh` there). `schema/CHANGELOG.md` is
  copied in by that script: don't edit it here.
- Pushing a client tag is a public release: confirm with the owner first.
- Before a pull request: `go run ./cmd/ows validate` and `go test ./...`.

## Changelog

`CHANGELOG.md` is the running record of this repository. Every change someone would notice adds an
entry under `## Unreleased` in the same pull request; work left undone goes under `## Still to do`
and comes off when it ships. On release, rename `Unreleased` to the release heading and start a new
empty one. `CONTRIBUTING.md` has the contributor version of this rule.

## Commits

Conventional commits (`type(scope): description`), on a branch with a pull request; never commit
to `main` directly. No attribution trailers.
