# Open Workout

An open format for a workout log, and the set of MCP tools an assistant uses to read and write one.

**Status: v0 development draft.** The schema may change until v1, and every change is listed in its changelog. v1 freezes when the first application outside Lifterclub writes it in production; after that, changes are additive.

| | |
| --- | --- |
| `schema/v0/workout.json` | The Open Workout Schema (OWS): one workout, its exercise entries and sets, where the record came from, and every inferred value listed by JSON pointer |
| `schema/v0/program.json` | A training program as a library publishes it |
| `schema/v0/routine.json` | A person's routine: named rows of an exercise and its targets, never performed work |
| `schema/v0/log.json` | A whole log as one file: the workouts, routines and deleted workouts, with room for a server's additions |
| `schema/v0/webhook-signature-vectors.json` | Worked cases for signing webhook deliveries (`OWS-Signature`) |
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
| `cmd/ows` | `ows validate`, which checks documents against the schemas; `ows conformance` and `ows conformance mcp`, which check a server's REST API and MCP tools; `ows vectors`, which checks a progression rules implementation; and `ows site`, which builds the website |
| `conformance` | The conformance scripts, the vectors runner and webhook signing as a Go package, for a server's own tests |

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

`ows conformance` runs fixed scripts against a server as one person, and `ows vectors` checks an implementation of the progression rules. Install it with:

```bash
go install github.com/openexercise/open-workout/cmd/ows@latest
```

### The REST API

`ows conformance` checks the [API core](api/core.yaml) with an OAuth access token carrying `workouts:read` and `workouts:write`:

```bash
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
| `change-feed` | `GET /v1/workouts?updated_since=…` lists the changed workout, valid |
| `export` | `GET /v1/export?format=json` holds the changed workout, and every workout in it is valid |
| `delete` | `DELETE` removes it; reading it is then a `not_found` problem (RFC 9457) |
| `tombstone` | The change feed with `include_deleted=true` lists it in `deleted`, with `deleted_at` and `updated_at`, and no longer as a workout |
| `stays-deleted` | Posting the deleted workout's `external_id` again is `not_found`, not a resurrection |
| `webhooks` | `POST /v1/webhooks` answers `201` with the signing `secret`; the list shows the subscription without it; `DELETE` removes it. Nothing is delivered |

The script logs one workout and deletes it at the end, also when a check fails part way, so use a test account. The webhook it subscribes points at `example.com` and is removed straight away. Errors are RFC 9457 problems whose `code` the report quotes; `--json` prints the report as JSON.

A delivery's signature can't be checked from outside, so `schema/v0/webhook-signature-vectors.json` gives worked cases: a sender signs each body at its timestamp and must get the case's `OWS-Signature`. `conformance.Sign` and `conformance.VerifySignature` implement the scheme in Go, for a receiver too.

### The MCP tools

`ows conformance mcp` checks an MCP server against the [tool profile](mcp/tools.json)'s core, with a credential an assistant would use and a read-only one for the same person:

```bash
OWS_TOKEN=… OWS_READ_TOKEN=… ows conformance mcp https://mcp.example.com
```

| Check | What it asks of the server |
| --- | --- |
| `discovery` | Without a credential, `401` with a `Bearer` challenge naming its protected resource metadata |
| `tools` | Every core tool is listed with the profile's input schema and annotations, its `_meta` marking it `core` |
| `log` | `log_workout` answers with a summary, then the result as JSON holding a valid workout |
| `replay` | The same call with the same `idempotency_key` returns the same workout |
| `needs-confirmation` | An exercise no catalogue knows comes back in `needs_confirmation` with its entry ID |
| `read` | `get_workouts` lists the workout, valid |
| `errors` | Failures are tool errors starting with a profile code: an unknown undo token is `undo_unavailable`, an unknown ID names a profile code |
| `read-only` | The read-only credential is shown only `readOnlyHint` tools, the core reads among them, and can't log |
| `delete` | `delete_entry` returns an `undo_token`, and `get_workouts` no longer lists the workout |

Without a read-only credential, `read-only` is skipped and the server doesn't pass.

### The progression rules

`ows vectors` runs every case in `schema/v0/progression-vectors.json` through a command you give it, once per case:

```bash
ows vectors -- ./my-progression-runner
```

The runner contract: the command reads one case on stdin, as JSON without `want`, and prints the target it works out on stdout: `{"load": 105, "needs_start": false}`, with `loads` (one per set group), `sets` (the next set scheme, for `stages`) and `exercise` (for `variations`) when it works them out. Only what a case's `want` states is compared. A Go implementation can call `conformance.CheckVectors` with its evaluator instead.

### Conformant servers

A server that passes may say so; it's self-reported, and the checks can't see everything (the profile's README lists what a server keeps to, and what an AI client does). Add yours to this list by pull request, with the version of `ows` it passed:

| Implementation | What passes | How |
| --- | --- | --- |
| [Lifterclub](https://lifterclub.com/developers) (reference) | REST, MCP, vectors, webhook signatures | In its CI, through `conformance.Run`, `RunMCP` and `CheckVectors` |
| [`mcp/template`](mcp/template) | MCP | In this repository's tests (`TestTemplatePasses`) |

A badge for a README:

```markdown
[![Open Workout conformant](https://img.shields.io/badge/Open%20Workout-conformant-2ea44f)](https://github.com/openexercise/open-workout#conformant-servers)
```

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
