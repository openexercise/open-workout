# Contributing

Open Workout is the Open Workout Schema (OWS), its MCP tool profile, the REST API core, the
conformance suite, a template server and two thin clients. Issues and pull requests are welcome.
This guide says how the repository is laid out, how a change gets in, and what to run before you
open a pull request.

If you want to use OWS rather than change it, read the [developer guide](docs/developer-guide.md)
and the [quickstarts](quickstarts/README.md).

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md). Report security problems
privately, as [SECURITY.md](SECURITY.md) says, never in a public issue. [GOVERNANCE.md](GOVERNANCE.md)
says who decides.

## Ways to help

- **Try it and report what's unclear or wrong.** An issue with the document or request that
  surprised you is useful on its own.
- **Propose a schema change** for workouts or clients the schema doesn't serve yet (below).
- **Run the conformance suite against your server** and report checks that are wrong or missing.
- **Improve the code that lives here**: `ows`, the conformance package, the template server, the
  example partner app, the quickstarts and the site.
- **List your server** as conformant, or register an extension vendor name, by pull request.

## Set up

You need git, Go (the version in `go.mod`) and, for the TypeScript client and the partner app,
Node 22.

```bash
git clone https://github.com/openexercise/open-workout.git
cd open-workout
go run ./cmd/ows validate
go test ./...
```

`go test ./...` builds and runs the template server (`TestTemplatePasses`), so it needs Go to
build `mcp/template` too. `go test -short ./...` skips that.

## How the repository is laid out

Until OWS v1, part of this repository is **published from Lifterclub**, the reference
implementation, by its `scripts/publish-openworkout.sh`. Each publish overwrites those paths, so a
change made only here would be lost at the next publish. The rest lives here and is changed here.

| Path | What it is | Source of truth |
| --- | --- | --- |
| `schema/v0/` | The JSON Schemas, `SPEC.md` and the vectors | Lifterclub, published here |
| `schema/CHANGELOG.md` | The schema's own changelog | Lifterclub, published here |
| `mcp/tools.json`, `mcp/README.md` | The MCP tool profile and what a server keeps to | Lifterclub, published here |
| `mcp/template/tools.json` | The template server's copy of the profile | Lifterclub, published here |
| `api/` | `core.yaml`, `v1.yaml`, `ofl-core.yaml` | Lifterclub, published here |
| `clients/go/`, `clients/ts/` | The Go and TypeScript clients (the whole directories) | Lifterclub, published here |
| `mcp/template/` (the rest) | The template server, a Go module of its own | Here |
| `cmd/ows/` | `ows validate`, `conformance`, `vectors` and `site` | Here |
| `conformance/` | The conformance checks as a Go package | Here |
| `internal/ows/` | The validator behind `ows validate` | Here |
| `schema.go` | Embeds the schemas and profile in the Go module | Here |
| `examples/` | Example documents, all checked by `ows validate` | Here |
| `quickstarts/`, `partner-app/` | Quickstarts and the example partner app | Here |
| `docs/` | The developer guide | Here |
| `README.md`, `CHANGELOG.md`, this file, `GOVERNANCE.md` | Project documents | Here |

At v1 the source of truth for everything moves to this repository.

## How a change gets in

### Changes to what lives here

Open a pull request against `main`. If the change is large, open an issue first so nobody does
the work twice.

### Changes to the schema, the profile, the API core or the clients

These are proposed and agreed **here**, then made in Lifterclub and published back:

1. **Open an issue** describing the change (what to include is below). Discussion happens in the
   issue. A maintainer decides after at least a week for comment ([GOVERNANCE.md](GOVERNANCE.md)).
2. **Optionally, open a pull request here** that edits the published files, as a concrete proposal
   reviewers can read and test. It isn't merged as it is: the next publish would overwrite it.
3. **A maintainer makes the agreed change in Lifterclub**, where the schema is tested against a
   real server's output, the API core and MCP profile are generated, and the clients are
   regenerated.
4. **A maintainer publishes it here** in a pull request (`chore: publish …`), with an entry in
   `CHANGELOG.md`. Your issue or proposal pull request is linked and closed.

Lifterclub's repository is private, so step 3 happens out of sight. The issue and the publish pull
request are where you see it land. You're credited in the changelog entry unless you'd rather not
be.

## Proposing a schema change

Write an issue that covers:

1. **The need.** What real workouts, apps or clients the change would serve. Include an example
   document as it would look with the change.
2. **Additive or breaking.** Additive: a new optional member, enum value or document. Breaking: a
   renamed or removed member, a changed unit or meaning, an optional member made required. Before
   v1 both can land, but breaking changes are rare and only to fix a mistake. After v1 only
   additive changes land without a new major version.
3. **What readers do with it.** How a reader that doesn't know the change behaves. By SPEC.md
   section 8 it ignores unknown members, treats an unknown set `type` as working and an unknown
   `match` as unconfirmed, and keeps what it can't fully read. If that would lose or misread data,
   say so.
4. **Alternatives.** Could an `x-<vendor>-…` extension do it first? Does a related standard
   (Open Exercise, Health Connect, HealthKit, FHIR) already hold it? SPEC.md section 9 lists what
   v0 leaves out on purpose, such as body metrics and GPS tracks.

The principles a change keeps to:

- **Record what the person stated.** A value a server would have to invent doesn't belong in the
  schema. Inferred values are listed in `estimates` by JSON pointer.
- **Every number has a stated unit**, and loads keep the unit the person gave.
- **Provenance travels with the record** (`source`, `external_id`, `merged_sources`).
- **An exercise is named by its Open Exercise ID**; names are what the person wrote. Changes to the
  exercise catalogue itself belong in
  [open-exercise](https://github.com/openexercise/open-exercise).

### What a complete change includes

When a change is made, all of these move together. A proposal pull request here should touch the
ones that apply, so reviewers can see the whole of it:

- **The schema** (`schema/v0/*.json`), with a `description` on every new member.
- **The specification** (`schema/v0/SPEC.md`), when the change affects meaning, identity or reader
  rules.
- **Examples** (`examples/`), so `ows validate` exercises the new shape. Every JSON file there
  must stay valid.
- **Vectors**, for a change to progression rules (`schema/v0/progression-vectors.json`: at least
  one case for each new behaviour) or to webhook signing (`schema/v0/webhook-signature-vectors.json`).
- **The API core and MCP profile** (`api/core.yaml`, `mcp/tools.json`), when servers must accept
  or return the change. These are generated in Lifterclub; a proposal can describe the change in
  words or as a hand-edited diff.
- **The clients**, when they expose the change (for example a new member on the Go client's
  `Workout`).
- **Conformance checks**, when servers must now behave differently (below).
- **The changelogs**: an entry for `schema/CHANGELOG.md` (dated, saying additive or breaking) and
  one under `Unreleased` in the root `CHANGELOG.md`.

## Adding or changing a conformance check

The checks live here, in `conformance/`, and are run by `ows conformance`, `ows conformance mcp`
and `ows vectors`. A new check raises the bar for every server, so open an issue first, and say
which behaviour of the API core or MCP profile it checks.

For a REST check:

1. Add its name to `Checks` in `conformance/conformance.go`, and its step, in the same position,
   to the `steps` in `Run`. If it needs an earlier check to have passed, say so in `blocked`, so it
   skips instead of failing.
2. In `conformance/conformance_test.go`, add a switch to the in-memory `reference` server that
   breaks the behaviour, and a case to `TestEachCheckCatchesItsBehaviour` showing that your check
   fails, and only the checks that depend on it.
3. Make sure the template server still passes (`TestTemplatePasses` in `conformance/mcp_test.go`
   runs it). If it doesn't, fix `mcp/template` in the same pull request.
4. Add the check to the table under "Check a server" in `README.md`, and an entry to
   `CHANGELOG.md`.

MCP checks follow the same pattern in `conformance/mcp.go` and `conformance/mcp_test.go`
(`TestMCPChecksCatchAMissingCore`).

Lifterclub runs these checks in its CI too, through this Go module, so a maintainer confirms a new
check passes there before Lifterclub moves to the new version.

## Listing a server or an extension vendor

- **Conformant servers:** add a row to the table in `README.md` under "Conformant servers", with
  what passes and the version of `ows` it passed. It's self-reported.
- **Extension vendors:** SPEC.md section 8 says `x-<vendor>-…` names are registered in the README.
  That list doesn't exist yet; the first registration adds it. Give the vendor name, who you are,
  and a link.

## Before you open a pull request

Run what CI runs (`.github/workflows/ci.yml`). At least:

```bash
go run ./cmd/ows validate
go test ./...
```

And for what you touched:

```bash
test -z "$(gofmt -l .)"                                    # Go formatting
go vet ./...
(cd clients/go && go vet ./... && go test ./...)            # the Go client, its own module
(cd mcp/template && go vet ./... && go test -race ./...)    # the template server, its own module
go run ./cmd/ows site dist                                  # the site still builds
(cd clients/ts && npm ci && npm run check && npm run typecheck && npm test && npm run build)
(cd partner-app && npm ci && npm run typecheck && npm test) # builds and runs the template server
```

`npm run check` in `clients/ts` fails when the generated types no longer match `api/core.yaml`,
`api/ofl-core.yaml` and `api/v1.yaml`.

## Commits and pull requests

- Work on a branch; never commit to `main` directly.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org):
  `type(scope): description`, such as `feat(conformance): check that undo restores IDs` or
  `docs(quickstarts): fix the Go token step`. Types: `feat`, `fix`, `docs`, `style`, `refactor`,
  `test`, `chore`, `perf`, `ci`, `build`.
- No attribution trailers in commit messages.
- One topic per pull request. Say what changed and why, link the issue, and say how you tested it.

## The changelog

`CHANGELOG.md` at the root is the running record of this repository. Every change someone would
notice (a feature, a fix, a change to the schema, profile, API or clients, a removal) adds an entry
under `## Unreleased` in the same pull request: one or two lines saying what changed and why it
matters, with the pull request number when known. Mark breaking changes **Breaking:**. Pure
refactors, tests and typo fixes don't need one.

Keep `## Still to do` current: add what you leave undone, and remove it when it ships.

`schema/CHANGELOG.md` is the schema's own changelog. It is published from Lifterclub; don't edit it
here.

## Releases

Releases are git tags, and a tag is a public release, so only the owner pushes them:

- **Go client:** `clients/go/v<version>` on a merge, found by `go get`.
- **TypeScript client:** `clients-ts-v<version>`, matching `version` in `clients/ts/package.json`.
  The Publish TypeScript client workflow checks the tag against the version, tests, and publishes
  to npm with provenance. The version is bumped in Lifterclub and published here first.

On release, `## Unreleased` in `CHANGELOG.md` becomes the release heading and a new empty
`## Unreleased` starts. The README's "Releasing the clients" has the steps.

## Licence

Contributions are under [Apache-2.0](LICENSE), the licence of this repository.
