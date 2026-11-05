# Template server

A small Open Workout server to copy: Go, the official MCP SDK, each person's log kept in memory.
Over one store it serves:

- the MCP profile's **core** tools (`../tools.json`, the ones whose `_meta` says
  `"dev.openworkout/profile": "core"`) at `/`, exactly as the profile lists them, with its
  instructions, keeping to what `../README.md` asks of a compatible server;
- the [API core](../../api/core.yaml) at `/v1`: workouts, entries and sets, undo, the change feed
  with tombstones and a cursor, export as a log document or CSV, signed webhooks, and exercise
  search.

It is a starting point, not a hosted service. [Lifterclub](https://lifterclub.com) is the
reference server, with the extensions as well.

```bash
cd mcp/template
OWS_TEMPLATE_TOKEN=dev-token OWS_TEMPLATE_READ_TOKEN=read-token go run . -addr localhost:8080 -unit kg -allow-http-webhooks
claude mcp add --transport http ows http://localhost:8080/ --header "Authorization: Bearer dev-token"
go test ./...
```

## What it does

| | |
| --- | --- |
| `server.go` | Reads the core tools from `tools.json` (embedded) and registers a handler for each; it refuses to start if the profile names a core tool it doesn't handle. Results are the summary, then the JSON, plus structured content; failures are tool errors whose text starts with a profile error code. Arguments are read strictly: unknown fields are `invalid_input` |
| `store.go` | Workouts, entries and sets as OWS v0; `log_workout`, `add_sets` (the session in progress: unfinished, started in the last 4 hours), `update_entry`, `delete_entry` and `undo` (10 minutes, original IDs); `idempotency_key` replays the first result and refuses a different request |
| `reads.go` | `get_workouts`, `get_personal_records` (heaviest load, Epley e1RM from 1–12 reps, best set volume; working sets of confirmed exercises only), `get_progress` and `search_exercises` |
| `catalogue.go`, `catalogue.json` | A dozen Open Exercise records and matching by name or alias, ignoring case, punctuation and plurals. Anything else is a close match with candidates, or unmatched, and is returned in `needs_confirmation` |
| `rest.go` | The API core: each route reads and writes the token's person's log; creates take an `Idempotency-Key` and replay an `external_id`, one the person deleted staying deleted; failures are RFC 9457 problems with the profile's codes |
| `webhooks.go` | Subscriptions and deliveries signed in `OWS-Signature` (`../../schema/v0/webhook-signature-vectors.json` has worked cases), with `OWS-Event` and `OWS-Delivery` |
| `main.go` | Serves it at `/`, stateless, behind two bearer tokens for one person: one that writes, one that only reads. Without a token, `401` names the protected resource metadata it serves (RFC 9728) |

A load without a unit is stored in the server's unit and listed in `estimates`, like a set the
assistant marked `estimated`. `log_workout`'s `program` is refused with `invalid_input`: programs
are an extension this server doesn't offer.

## Making it real

- **Storage:** replace `Store` with a database. Scope every query by person, write each change in
  one transaction, keep idempotency keys for at least a day, and keep records in a table rebuilt
  when an exercise's sets change.
- **Webhooks:** deliver from a queue that retries, check on every delivery that the address
  reached is public (the template doesn't, and `-allow-http-webhooks` is for local work only), and
  pause a subscription that keeps failing.
- **Sign-in:** replace the token with OAuth 2.1 (PKCE, dynamic registration or client ID metadata
  documents), answer a missing token with `401` and `WWW-Authenticate: Bearer
  resource_metadata=…` (RFC 9728) so clients find the authorization server, and name the person
  in `TokenInfo.UserID`.
- **Read-only access:** kept here as the template does it (`limitToScope`): a credential with only
  `mcp:read` sees and calls only the tools annotated `readOnlyHint`, and others are a protocol
  error starting `read_only`.
- **The catalogue:** load the whole of [Open Exercise](https://openexercise.org), resolve renamed
  IDs through `previous_ids`, and keep each person's own exercises.
- **Limits:** count calls per person and answer `rate_limited`.
- **Consent:** if the law where you operate requires consent to store health data, refuse writes
  with `consent_required` until it's given; reads can still work.

It passes `ows conformance mcp` and `ows conformance` (`TestTemplatePasses` in `conformance`,
which builds and runs it), and the [example partner app](../../partner-app) runs against it in CI:

```bash
OWS_TOKEN=dev-token OWS_READ_TOKEN=read-token ows conformance mcp http://localhost:8080/
OWS_TOKEN=dev-token ows conformance http://localhost:8080
```

`tools.json` here is a copy of `../tools.json`, so the template can be copied out alone;
`TestCopyOfTheProfileIsCurrent` fails while they differ.
