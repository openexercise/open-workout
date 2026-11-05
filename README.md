# Open Workout

An open format for a workout log, and the set of MCP tools an assistant uses to read and write one.

**Status: v0 development draft.** The schema may change until v1, and every change is listed in its changelog. v1 freezes when the first application outside Lifterclub writes it in production; after that, changes are additive.

| | |
| --- | --- |
| `schema/v0/workout.json` | The Open Workout Schema (OWS): one workout, its exercise entries and sets, where the record came from, and every inferred value listed by JSON pointer |
| `schema/v0/program.json` | A training program as a library publishes it |
| `schema/v0/routine.json` | A person's routine: named rows of an exercise and its targets, never performed work |
| `schema/v0/log.json` | A whole log as one file: the workouts, routines and deleted workouts, with room for a server's additions |
| `schema/v0/progression-vectors.json` | Worked cases for the progression rules in `program.json`: a slot and the sessions logged for it, and the target an implementation should work out next |
| `schema/CHANGELOG.md` | What changed in v0 |
| `mcp/tools.json` | The OWS MCP tool profile: the tools, their input schemas and annotations, generated from the reference server |
| `mcp/README.md` | What a server offering those tools keeps to |
| `mcp/template` | A template server to copy: Go, the official MCP SDK, an in-memory store, serving the profile's core tools. Its own module |
| `examples` | A workout and a program in the format |
| `api/core.yaml` | The Open Workout API core as OpenAPI 3.1: the routes any Open Workout server serves (workouts, sets and entries, undo, the change feed, export, webhooks, exercise search); workouts refer to `schema/v0/workout.json` |
| `api/v1.yaml` | Lifterclub's whole API, the reference implementation: the core plus its own extensions |
| `clients/go` | A thin Go client, package `openworkout`, standard library only: the module `github.com/openexercise/open-workout/clients/go` |
| `clients/ts` | A thin TypeScript client, `@openexercise/open-workout-client`: the core at its root, Lifterclub's API at `/lifterclub` |
| `cmd/ows` | `ows validate`, which checks documents against the schemas; `ows conformance`, which checks a server's REST API; and `ows site`, which builds the website |
| `conformance` | The conformance script as a Go package, for a server's own tests |

Until v1 these files are published here from [Lifterclub](https://lifterclub.com), the reference implementation, and the schema is validated against real output there. Issues and proposals belong here.

## Use it

```bash
go run ./cmd/ows validate            # the examples
go run ./cmd/ows validate my-log.json # your own documents, files or directories
go test ./...
go run ./cmd/ows site dist           # the website into dist
```

A document names its schema in `$schema`; without one it is checked as a workout.

The site at [openworkout.dev](https://openworkout.dev) is built from this repository by `ows site` and deployed by the Pages workflow on every push to `main`, so the schemas resolve at their `$id` URLs.

## Check a server

`ows conformance` runs a fixed script against a server offering the reference REST API (`/v1`, as [Lifterclub](https://lifterclub.com/developers) documents it), as one person with an OAuth access token carrying `workouts:read` and `workouts:write`:

```bash
go install github.com/openexercise/open-workout/cmd/ows@latest
OWS_TOKEN=… ows conformance https://api.example.com
```

| Check | What it asks of the server |
| --- | --- |
| `discovery` | Without a token, `401` with a `Bearer` challenge naming its protected resource metadata (RFC 9728) |
| `create` | `POST /v1/workouts` with an `Idempotency-Key` and an `external_id` answers `201` with `Location` and a valid workout holding what was sent: start, title, notes, `source.external_id`, each exercise's sets with loads in the unit given |
| `read` | `GET` it back, valid and equal to what `create` returned |
| `replay` | The same key and body answer `200` with `Idempotent-Replayed: true` and the same workout |
| `external-id` | A new key with the same `external_id` replays too |
| `update` | `PATCH` changes the title and notes; `updated_at`, when kept, doesn't go backwards |
| `export` | `GET /v1/export?format=json` holds the changed workout, and every workout in it is valid |
| `delete` | `DELETE` removes it; reading it is then a `not_found` problem (RFC 9457) |
| `stays-deleted` | Posting the deleted workout's `external_id` again is `not_found`, not a resurrection |

The script logs one workout and deletes it at the end, also when a check fails part way, so use a test account. Errors are RFC 9457 problems whose `code` the report quotes; `--json` prints the report as JSON. A server that passes may call itself conformant; the badge is self-reported. A server written in Go can run the same script in its tests with `conformance.Run`; Lifterclub's CI does.

## Clients

Both are thin: the access token on every request, an `Idempotency-Key` on every POST, RFC 9457 problems as errors with `Retry-After` when rate limited, and paging over the workout list and the change feed. Getting the token (OAuth 2.1 with PKCE) is the app's job. Each has a README.

```bash
go get github.com/openexercise/open-workout/clients/go
```

```bash
npm install @openexercise/open-workout-client
```

`npm run check` in `clients/ts` fails when its generated types no longer match `api/core.yaml` and `api/v1.yaml`. Up to 0.3.0 the TypeScript client was `@openexercise/lifterclub-client`; its `/lifterclub` entry replaces that package unchanged.

### Releasing the clients

The Go client is a module of its own: tag a merge `clients/go/v<version>` (`git tag clients/go/v0.1.0`) and push the tag, and `go get` finds it.

The TypeScript client:

The package and its version come from Lifterclub, which publishes them here. To release one:

1. Bump `version` in `clients/ts/package.json` there, run its publish script into this repository, and merge.
2. Tag the merge here and push the tag: `git tag clients-ts-v0.4.0 && git push origin clients-ts-v0.4.0`.
3. The Publish TypeScript client workflow checks the tag against `package.json`, tests, and publishes with provenance, through npm's trusted publishing: no token is stored in this repository.

## Relationship to other standards

An OWS entry refers to its exercise by an [Open Exercise](https://github.com/openexercise/open-exercise) ID (`oec:barbell-bench-press`). Open Exercise says what an exercise is; Open Workout records what someone did.

## Taking part

Issues and pull requests are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md), [GOVERNANCE.md](GOVERNANCE.md) for who decides and how, the [code of conduct](CODE_OF_CONDUCT.md), and [SECURITY.md](SECURITY.md) for reporting vulnerabilities privately.

## Licence

[Apache-2.0](LICENSE).
