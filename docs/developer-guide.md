# Developer guide

How to write and read Open Workout Schema (OWS) data, and how to build a server that serves it.
It is for app developers, people building an MCP server or a REST API over a workout log, and
anyone importing or exporting a log.

The rules live elsewhere, and win when this guide disagrees with them:

- [`schema/v0/SPEC.md`](../schema/v0/SPEC.md): what the members mean and how documents change.
- [`schema/v0/*.json`](../schema/v0): the JSON Schemas (draft 2020-12), normative for shape.
- [`api/core.yaml`](../api/core.yaml): the REST API every Open Workout server serves.
- [`mcp/tools.json`](../mcp/tools.json) and [`mcp/README.md`](../mcp/README.md): the MCP tool
  profile.

For something running in a few minutes, start with the [quickstarts](../quickstarts/README.md).
This guide is the longer read behind them.

OWS is a v0 draft. It may change until v1, and every change is listed in
[`schema/CHANGELOG.md`](../schema/CHANGELOG.md).

## Contents

1. [The pieces](#1-the-pieces)
2. [A workout, member by member](#2-a-workout-member-by-member)
3. [Exercises and matches](#3-exercises-and-matches)
4. [Sets, units and device readings](#4-sets-units-and-device-readings)
5. [Estimates](#5-estimates)
6. [Time: started_at, date and timezone](#6-time-started_at-date-and-timezone)
7. [Provenance](#7-provenance)
8. [Extensions](#8-extensions)
9. [Versions and reader rules](#9-versions-and-reader-rules)
10. [Routines](#10-routines)
11. [Logs and exports](#11-logs-and-exports)
12. [Programs and progression rules](#12-programs-and-progression-rules)
13. [Validating documents](#13-validating-documents)
14. [The REST API core](#14-the-rest-api-core)
15. [Webhooks and signatures](#15-webhooks-and-signatures)
16. [The MCP profile](#16-the-mcp-profile)
17. [Building a compatible server](#17-building-a-compatible-server)
18. [The clients](#18-the-clients)

## 1. The pieces

| Piece | What it is | Where |
| --- | --- | --- |
| Workout | One session: entries, sets, when, and where the record came from | `schema/v0/workout.json` |
| Routine | A person's plan for a session: targets, never performed work | `schema/v0/routine.json` |
| Program | What a program prescribes, as rules for loads | `schema/v0/program.json` |
| Log | A whole log as one file: workouts, routines, deleted workouts | `schema/v0/log.json` |
| Progression vectors | Worked cases for the program rules | `schema/v0/progression-vectors.json` |
| Signature vectors | Worked cases for signing webhook deliveries | `schema/v0/webhook-signature-vectors.json` |
| API core | REST routes over a person's log | `api/core.yaml` |
| MCP profile | Tools an assistant uses over a log | `mcp/tools.json` |
| `ows` | Validate documents, check a server, check a rules implementation | `cmd/ows` |
| `conformance` | The same checks as a Go package | `conformance/` |
| Template server | A small server to copy, with both the API core and the MCP core | `mcp/template` |
| Clients | Go (`clients/go`) and TypeScript (`clients/ts`) | `clients/` |

Exercises are not defined here. An entry names its exercise by an
[Open Exercise](https://openexercise.org) ID, such as `oec:barbell-bench-press`. Open Exercise says
what an exercise is; OWS records what someone did.

## 2. A workout, member by member

[`examples/push-day.json`](../examples/push-day.json) is a small workout:

```json
{
  "$schema": "https://openworkout.dev/schema/v0/workout.json",
  "id": "wk_01JC9Z6X3T8Q2",
  "started_at": "2026-10-27T17:42:00-05:00",
  "ended_at": "2026-10-27T18:31:00-05:00",
  "title": "Push day",
  "source": { "kind": "mcp", "client": "claude", "external_id": null },
  "exercises": [
    {
      "exercise": { "id": "oec:barbell-bench-press", "name": "Barbell Bench Press", "match": "catalogue" },
      "sets": [
        { "reps": 8, "load": { "value": 185, "unit": "lb" }, "rpe": 8 },
        { "reps": 8, "load": { "value": 185, "unit": "lb" } },
        { "reps": 7, "load": { "value": 185, "unit": "lb" } }
      ]
    },
    {
      "exercise": { "id": "oec:dip", "name": "Dip", "match": "close" },
      "sets": [ { "reps": 10 }, { "reps": 10 }, { "reps": 8 } ]
    }
  ],
  "estimates": [],
  "notes": "Shoulder felt fine"
}
```

[`examples/lower-day.json`](../examples/lower-day.json) uses more of the schema: a local day and
time zone, set types, an unmatched entry, cardio with device readings and an estimate.

Only `started_at`, `source` and `exercises` are required. Unknown members are refused, except
`x-` extensions (section 8).

| Member | Type | Meaning |
| --- | --- | --- |
| `$schema` | string | `https://openworkout.dev/schema/v0/workout.json` when present |
| `ows_version` | `"MAJOR.MINOR"` | The version the document was written to; absent means 0.0 (section 9) |
| `id` | string | Assigned by the server that stores it. Opaque: never parse it |
| `started_at` | date-time with offset | When it started. Required |
| `ended_at` | date-time | When it ended |
| `date` | `YYYY-MM-DD` | The local day: `started_at` in `timezone` |
| `timezone` | IANA zone | Where it happened, such as `America/Chicago` |
| `updated_at` | date-time | When the stored record last changed, by the server's clock. For mirroring |
| `title`, `notes` | string | The person's own text |
| `source` | object | Where the record came from (section 7). Required |
| `merged_sources` | array of `source` | Other apps' records of the same session, merged into this one |
| `exercises` | array of entries | In the order done. Required |
| `program` | object | The program day this workout was part of: a label only |
| `estimates` | array of JSON pointers | Values that were inferred, not given (section 5) |

An entry:

| Member | Meaning |
| --- | --- |
| `id` | Assigned by the server; used to edit the entry |
| `exercise` | `{id, name, match}`: what the entry was matched to (section 3). Required |
| `text` | What the person called the exercise, in their words |
| `notes` | The person's notes |
| `sets` | The sets, in order. Required |

A set has no required members. Section 4 covers them.

The `program` label has `listed` (required), `id`, `name`, `version`, `day` and `day_name`.
`listed: false` means the program was withdrawn; a reader then shows nothing more about it. The
label changes nothing else in the workout.

## 3. Exercises and matches

An entry's `exercise` has three members, all required:

- `id`: the exercise's ID, or `null` when nothing matched.
- `name`: the exercise's name (the catalogue's name for a catalogue match, or the person's words
  when unmatched).
- `match`: how sure the server is.

| `match` | Confirmed | Meaning |
| --- | --- | --- |
| `catalogue` | yes | An Open Exercise record |
| `custom` | yes | The person's own exercise |
| `close` | no | A likely match that waits for the person to confirm |
| `unmatched` | no | Nothing matched; `id` is `null` |

Unconfirmed entries (`close`, `unmatched`) MUST NOT count toward records until the person
confirms them. Keep the person's own words in `text`, so the entry can be confirmed later.

ID namespaces:

- `oec:<slug>`: an Open Exercise Catalogue record. Only the catalogue mints these. A server MUST NOT
  invent an `oec:` ID.
- `usr:<…>`: a person's own exercise.
- A server's own prefix, such as the reference server's `lc:`, for an exercise it added itself.
  The catalogue can adopt it later under an `oec:` ID.

The catalogue renames an exercise by listing the old ID in the new record's `previous_ids`. A
reader that keeps workouts under an old ID should move them to the new one, and must still resolve
the old one. See SPEC.md section 3.

To find IDs, use the catalogue at [openexercise.org](https://openexercise.org), a server's
exercise search (`GET /v1/exercises?query=…`, or the MCP tool `search_exercises`), or the
[matching quickstart](../quickstarts/matching.md).

## 4. Sets, units and device readings

| Member | Unit | Meaning |
| --- | --- | --- |
| `id` | | Assigned by the server; used to edit the set |
| `type` | | `working` (the default), `warmup`, `backoff`, `amrap`, `failure` or `drop` |
| `reps` | count | Integer, 0 or more |
| `load` | `{value, unit}` | Weight lifted. `unit` is `kg` or `lb`. Absent for bodyweight |
| `rpe` | 1–10 | Rate of perceived exertion |
| `duration_s` | seconds | Integer. Holds and cardio |
| `distance_m` | metres | Cardio |
| `calories` | kcal | Integer, as a device or app reported it |
| `heart_rate_bpm` | beats per minute | Integer, the set's average, as a device or app reported it |

Rules:

- Only `warmup` sets are left out of records.
- A load keeps the unit the person gave. A reader converts for display, and MUST NOT store the
  converted number as if it were given.
- A set records totals. GPS tracks and per-second samples stay in the file they came from (FIT,
  GPX, TCX).

## 5. Estimates

`estimates` lists JSON pointers ([RFC 6901](https://www.rfc-editor.org/rfc/rfc6901)) to values that
were inferred rather than given. A number not listed was given by the person. A writer MUST NOT
invent a number without listing it.

A pointer can name:

- a whole set: `/exercises/0/sets/2` (everything in it was inferred, such as values read off a
  machine's screen);
- one value in a set: `/exercises/0/sets/2/reps`;
- a set's unit: `/exercises/1/sets/0/load/unit` (the person gave a number, and the unit was
  assumed);
- the start: `/started_at` (the writer knew only the day; trust the day, not the hour).

In `examples/lower-day.json`, `"/exercises/1/sets/0/load/unit"` says the person said "60", and the
server assumed kilograms.

Pointers index into arrays by position, so a writer that reorders entries or sets must rewrite its
pointers.

## 6. Time: started_at, date and timezone

- `started_at` and `ended_at` are instants with an offset (`2026-11-10T18:05:00-06:00`).
- `timezone` is the IANA zone the workout happened in. It keeps the workout's day stable across
  daylight saving changes and when the person moves.
- `date` is `started_at` read in `timezone`. A reader files the workout under `date`, wherever the
  reader is.

When a writer knows only the day, a server works out a start and lists `/started_at` in
`estimates`. In the API core, a write may send `date` instead of `started_at` for that reason.

## 7. Provenance

`source` says where the record came from:

| Member | Meaning |
| --- | --- |
| `kind` | `app`, `mcp`, `api` or `import`. Required |
| `client` | The app or client, such as `claude`, `strong` or `hevy` |
| `external_id` | The writing app's own ID for the workout, or `null` |

A server that stores workouts from several apps keys `external_id` per app: the same
`external_id` from the same app is the same workout. In the API core, a server takes `kind` and
`client` from the credential, not from the body.

When a server merges records of one session from two apps (a run recorded by a watch and a phone,
say), the fullest record stays, and the others are listed in `merged_sources`. The hidden ones
appear in a log's `deleted_workouts` with `merged_into`.

## 8. Extensions

A producer may add `x-<vendor>-<name>` members to a workout, an entry, a set, a routine, a routine
row, a program, a day or a slot. Consumers MUST ignore `x-` members they don't understand. A server
that reads a write may drop them.

```json
{
  "exercise": { "id": null, "name": "zercher squat", "match": "unmatched" },
  "text": "zercher squat",
  "sets": [ { "reps": 8, "load": { "value": 60, "unit": "kg" } } ],
  "x-myapp-machine": "rack 3"
}
```

`<vendor>` is your producer name. SPEC.md section 8 says vendor names are registered in this
repository's README by pull request; that list doesn't exist yet, so the first registration adds
it. An extension that proves useful to more than one producer can be proposed for the core
([CONTRIBUTING.md](../CONTRIBUTING.md)).

Don't use an extension to carry a value the schema already has a member for, and don't use one to
carry an invented number. The estimates rule applies to extensions too.

## 9. Versions and reader rules

- `ows_version` is `MAJOR.MINOR`, such as `"0.1"`. A document without it is 0.0, the drafts before
  versions were numbered.
- The schemas' `$id`s stay at `…/schema/v0/` for every 0.x, and always serve the latest 0.x.
- A minor version only adds: new optional members, enum values and documents. It never removes or
  renames a member, never changes a unit or a meaning, and never makes an optional member required.

A reader meeting a newer document keeps it:

- it ignores members it doesn't know;
- a set `type` it doesn't know is a working set;
- a `match` it doesn't know is unconfirmed;
- it never drops a workout, entry or set it can't fully read.

If you validate against a local copy of the schemas, it goes stale with the next minor version.
Refresh it rather than rejecting a newer document.

Until v1, a breaking change is possible but rare, and is named as breaking in the changelog. v1
freezes when the first app outside Lifterclub writes OWS in production. After that, breaking
changes need a new major version and new `$id`s.

## 10. Routines

A routine is a person's own plan for a session. Its rows are targets, never performed work. When
the person trains from it, the result is a workout.

Required: `name` and `exercises`. Each row requires `exercise_id` and `name`; every target is
optional, so a plan may name only the exercise.

| Row member | Meaning |
| --- | --- |
| `exercise_id` | An `oec:` ID or another namespace's, such as `usr:` |
| `name` | The exercise's name |
| `equipment` | The exercise's equipment, as the server's catalogue lists it |
| `sets` | Number of sets, 1 or more |
| `reps_min`, `reps_max` | A rep target, or a range when both are given |
| `load`, `unit` | A target weight and its unit (`kg` or `lb`) |
| `duration_s`, `distance_m` | Time and distance targets |
| `rest_seconds` | Rest between sets |
| `notes` | The person's notes |
| `progression` | How the next weight is suggested: `{type, increment}` |

A row's `progression.type` is `linear` (add `increment` when every set reaches `reps_min`),
`double` (work up the rep range, then add `increment`) or `off`. Absent means the server's default
for the row's shape.

A routine copied from a program library carries `source` (`program_id`, `program_name`, `credit`,
`version`). It is removed when that program is withdrawn.

## 11. Logs and exports

A log is a whole log as one file: what an export holds and an import can read.

```json
{
  "$schema": "https://openworkout.dev/schema/v0/log.json",
  "ows_version": "0.1",
  "exported_at": "2026-11-13T00:00:00Z",
  "workouts": [],
  "routines": [],
  "deleted_workouts": []
}
```

- `exported_at` and `workouts` are required.
- `workouts` are OWS workouts, newest first.
- `routines` are OWS routines.
- `deleted_workouts` items are `{deleted_at, merged_into?, workout}`: workouts the person deleted,
  or that a merge hid.
- A server may add more of the person's record under keys the schema doesn't name, such as a
  profile. Readers ignore keys they don't know.

The API core's `GET /v1/export?format=json` returns a log document. `format=csv` returns one row per
set.

## 12. Programs and progression rules

A program document says what a program prescribes, week by week. Loads are rules, not weights, so
the same program gives each person their own numbers.
[`examples/two-day-linear.json`](../examples/two-day-linear.json) is a two-day linear program.

Structure: `weeks` → `days` (each with a `key` and `name`) → `slots`. A slot requires `exercise`,
`sets` (set groups such as `{"count": 5, "reps_min": 5}`) and `load`, and may carry `progression`,
`rest_seconds`, `substitutes` and `notes`.

`load.type` is one of:

| `type` | Meaning |
| --- | --- |
| `last` | The load used last time on this slot, moved by the slot's progression rule |
| `percent_training_max` | A percentage of a training max, optionally another lift's (`of`) |
| `percent_of_slot` | A percentage of another slot's current target (`value`, `day`, optional `of`) |
| `rpe` | Work to an RPE (`value`) |
| `choice` | The person's own pick |

`progression.type` is one of `linear`, `double`, `increment_table`, `stages` and `variations`,
with an optional `on_fail` (for example `{"after_misses": 3, "drop_percent": 10}`). The full rules,
including what counts as done and short, are in the `progressionRule` description in
`schema/v0/program.json`. Missing data holds: a session with unknown reps on a set the rule reads is
neither a success nor a miss.

### The progression vectors

`schema/v0/progression-vectors.json` holds 68 worked cases. Each case has a `name`, a `unit`, a
`slot`, the `sessions` logged for it (oldest first) and the `want`ed target. If you implement the
rules, run your implementation against every case.

From the command line, `ows vectors` runs your command once per case. It sends the case on stdin as
JSON without `want`, and reads the target from stdout:

```json
{"load": 105, "needs_start": false}
```

Add `loads` (one per set group), `sets` (the next set scheme, for `stages`) and `exercise` (for
`variations`) when you work them out. Only what a case's `want` states is compared, beyond `load`
and `needs_start`.

A runner that always answers "no weight yet" shows the contract. Save it as `runner.sh` and make it
executable:

```sh
#!/bin/sh
cat > /dev/null
echo '{"load": null, "needs_start": true}'
```

```bash
go run ./cmd/ows vectors -- ./runner.sh
```

It passes the two cases that want a starting number and fails the rest:

```
FAIL linear total_reps: a set that went further makes up for one short: load = none, want 105
...
68 cases: 2 passed, 66 failed, 0 skipped
the implementation disagrees with the published cases
```

`--json` prints the report as JSON. In Go, call `conformance.CheckVectors` with your evaluator
instead; see section 17.

## 13. Validating documents

From a checkout of this repository, or anywhere with the installed command
(`go install github.com/openexercise/open-workout/cmd/ows@latest`):

```bash
go run ./cmd/ows validate                      # everything under examples/
go run ./cmd/ows validate path/to/workout.json # files or directories
```

A document names its schema in `$schema`: a workout, routine, program or log. Without one it is
checked as a workout. The schemas are built into the command, so it needs no checkout. A valid run
prints `N document(s) valid`. An invalid one says where and why, and exits 1:

```
bad.json: jsonschema validation failed with 'https://openworkout.dev/schema/v0/workout.json#'
- at '/exercises/0/sets/0': validation failed
  - at '/exercises/0/sets/0/load/unit': value must be one of 'kg', 'lb'
  - at '/exercises/0/sets/0': additional properties 'weight' not allowed
1 problem(s) in 1 document(s)
```

`examples/` holds one of each: `push-day.json` and `lower-day.json` (workouts),
`push-routine.json`, `two-day-linear.json` (a program) and `log.json`.

In Go, the `conformance` package validates the documents a server returns. The schemas are also
embedded in the module root as `openworkout.Schemas` (package `github.com/openexercise/open-workout`),
for your own validator.

## 14. The REST API core

[`api/core.yaml`](../api/core.yaml) (rendered at [openworkout.dev/api](https://openworkout.dev/api/))
lists the routes every Open Workout server serves. A server may add its own routes and fields;
clients ignore what they don't know. [`api/v1.yaml`](../api/v1.yaml) is Lifterclub's whole API: the
core plus its extensions.

The examples below ran against the template server on `localhost:8080`:

```bash
cd mcp/template
OWS_TEMPLATE_TOKEN=dev-token OWS_TEMPLATE_READ_TOKEN=read-token go run . -allow-http-webhooks
```

### Finding the server and signing in

Without a token, a server answers `401` with a challenge naming its protected resource metadata
([RFC 9728](https://www.rfc-editor.org/rfc/rfc9728)):

```bash
curl -si http://localhost:8080/v1/workouts
```

```
HTTP/1.1 401 Unauthorized
Www-Authenticate: Bearer resource_metadata="http://localhost:8080/.well-known/oauth-protected-resource"
```

That document names the authorization server ([RFC 8414](https://www.rfc-editor.org/rfc/rfc8414)).
Clients use OAuth 2.1 with PKCE (S256). The scopes are `workouts:read` and `workouts:write`.
[`partner-app/src/oauth.ts`](../partner-app/src/oauth.ts) is a working sign-in to copy. The template
server uses fixed tokens instead.

### Writing a workout

`POST /v1/workouts` takes a `WorkoutInput`. Send an `Idempotency-Key` header (any unique string up
to 200 characters) on every create:

```bash
curl -s -X POST http://localhost:8080/v1/workouts \
  -H 'Authorization: Bearer dev-token' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: guide-1' \
  -d '{
    "external_id": "myapp-42",
    "started_at": "2026-11-10T18:00:00-06:00",
    "title": "Lower",
    "exercises": [
      {"exercise_id": "oec:barbell-back-squat", "sets": [{"reps": 5, "load": 100, "unit": "kg"}]},
      {"name": "zercher squat", "sets": [{"reps": 8, "load": 60, "estimated": true}]}
    ]
  }'
```

The answer is `201 Created` with `Location: /v1/workouts/{id}` and a `Logged` body:

- `workout`: the stored OWS workout, with the server's IDs, `updated_at` and `estimates`.
- `needs_confirmation`: entries to confirm with the person, each with `entry_id`, `text`, `status`
  and `candidates`.

In the example, "zercher squat" came back as a `close` match in `needs_confirmation`, and
`estimates` listed `/exercises/1/sets/0` (marked `estimated`) and `/exercises/1/sets/0/load/unit`
(no unit was given).

What a write accepts:

- An entry by `exercise_id` (skips matching), by `name` (the person's words, matched by the server),
  or as an OWS entry (`exercise` and `text`).
- A load as a number with `unit` beside it, or as an OWS load `{value, unit}`.
- A set's `type`, or `warmup: true`; `estimated: true` for a set whose values were inferred.
- `estimates` as JSON pointers into the body; `date` instead of `started_at` when only the day is
  known; `timezone`.
- An OWS workout document as it is. The members the server assigns (`id`, `updated_at`,
  `merged_sources`, `source.kind` and `client`, entry and set `id`s, `match`) are ignored.

A document's `exercise.id` is a sure match only when its `match` is `catalogue` (or missing) and
the server knows the ID; otherwise the server matches the entry's `text`, as it would a name. The
`document` conformance check posts a document like this.

Retries:

- The same `Idempotency-Key` returns the first result: `200` with `Idempotent-Replayed: true`.
- The same `external_id` from the same app also replays, even with a new key. Use it to make a sync
  safe to repeat.
- An `external_id` the person deleted on the server answers `404 not_found` and stays deleted. A
  sync must not bring it back.

### Confirming, editing and deleting

| Route | Use |
| --- | --- |
| `PATCH /v1/entries/{id}` | Confirm an entry with `{"exercise_id": "…"}`, or change its `notes` |
| `PATCH /v1/workouts/{id}` | Change `title`, `date` or `notes`, or finish it with `ended_at` |
| `POST /v1/workouts/{id}/sets` | Add sets; `{id}` may be `current`, the session in progress |
| `POST /v1/entries/{id}/sets` | Add sets to one entry |
| `PATCH /v1/sets/{id}`, `DELETE /v1/sets/{id}` | Edit or delete a set |
| `DELETE /v1/workouts/{id}`, `DELETE /v1/entries/{id}` | Delete; the answer carries an `undo_token` |
| `POST /v1/undo` | Restore with `{"undo_token": "…"}`, within 10 minutes |

With the `entry_id` from `needs_confirmation`:

```bash
curl -s -X PATCH "http://localhost:8080/v1/entries/$ENTRY_ID" \
  -H 'Authorization: Bearer dev-token' -H 'Content-Type: application/json' \
  -d '{"exercise_id": "oec:barbell-back-squat"}'
```

The entry's `match` becomes `catalogue` and it leaves `needs_confirmation`.

### Reading and mirroring

`GET /v1/workouts` lists workouts newest first, by `from` and `to` (days in the person's time
zone), `exercise`, `limit` (up to 100) and `cursor`. Follow `next_cursor` while `truncated` is
true.

The change feed mirrors a log. Pass `updated_since` and, to see deletes, `include_deleted=true`:

```bash
curl -s 'http://localhost:8080/v1/workouts?updated_since=2026-11-01T00:00:00Z&include_deleted=true' \
  -H 'Authorization: Bearer dev-token'
```

After the only workout in the log was deleted (`DELETE /v1/workouts/{id}`), it answered:

```json
{"updated_since":"2026-11-01T00:00:00Z","workouts":[],"deleted":[{"id":"wk_2hcqnjp2qmqzybvs","deleted_at":"2026-11-13T16:06:39.606866-06:00","updated_at":"2026-11-13T16:06:39.606866-06:00"}],"truncated":false}
```

(The template also sends `totals` and `summary`, which aren't part of the core.)

- Changes come oldest first, ignoring `from`, `to` and `exercise`.
- `deleted` holds tombstones: `id`, `deleted_at`, `updated_at`, and `merged_into` when a merge hid
  the workout. Remove your copy.
- Store the latest `updated_at` you've seen and ask from there next time.

`GET /v1/exercises?query=…` searches exercises (`equipment`, `muscle` and `limit` narrow it).
`GET /v1/export?format=json` returns a log document (section 11).

### Errors

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problems (`application/problem+json`)
with a `code`:

```json
{"code":"not_found","detail":"the workout with this external_id was deleted, and stays deleted","status":404,"title":"Not Found","type":"about:blank"}
```

The core's codes are `invalid_input` (with `field`), `consent_required`, `exercise_ambiguous`
(with `candidates`), `not_found`, `undo_unavailable`, `rate_limited` (with `Retry-After`),
`idempotency_conflict`, `unauthenticated` and `internal_error`. A server may add codes of its
own.

## 15. Webhooks and signatures

`POST /v1/webhooks` with `{"url": "https://…", "events": ["workout.created", "workout.updated",
"workout.deleted"]}` subscribes the calling app. The `201` response holds the signing `secret`,
shown once. `GET /v1/webhooks` lists subscriptions without it; `DELETE /v1/webhooks/{id}` ends one.

A delivery is a POST whose body names the change, never the workout's content:

```json
{"event":"workout.created","workout_id":"0b0c5f2e-6f1d-4a58-9a4e-2a1e9f5c1d77","occurred_at":"2026-11-05T12:00:00Z","subscription_id":"6c1f4f0e-3d0b-4a33-8f5e-1f0d2a9b7c11","delivery_id":"d2b1c0a9-8e7f-4d6c-b5a4-938271605f4e"}
```

Headers: `OWS-Event`, `OWS-Delivery` (drop repeats by it) and `OWS-Signature`. Read the workout
afterwards with `GET /v1/workouts/{id}`.

`OWS-Signature` is `t=<unix seconds>,v1=<hex HMAC-SHA256>`, keyed with the secret's UTF-8 bytes,
over `<t>.<body>`: the timestamp, a full stop, and the raw body exactly as sent. A receiver
recomputes it, compares in constant time, and refuses a timestamp too far from its own clock (five
minutes is usual). Check the raw bytes, before any JSON parsing.

The first case in `schema/v0/webhook-signature-vectors.json`, recomputed with `openssl`:

```bash
body='{"event":"workout.created","workout_id":"0b0c5f2e-6f1d-4a58-9a4e-2a1e9f5c1d77","occurred_at":"2026-11-05T12:00:00Z","subscription_id":"6c1f4f0e-3d0b-4a33-8f5e-1f0d2a9b7c11","delivery_id":"d2b1c0a9-8e7f-4d6c-b5a4-938271605f4e"}'
printf '%s' "1793980800.$body" | openssl dgst -sha256 -hmac whsec_example
```

The digest is `ca18039599be0f8e28ecb93c9aa0c342c65a7cc17d046cf916a2bbf0ef41d04e`, the case's `v1`.
Every case, in Node, run from the repository root (`node check-signatures.cjs`):

```js
const { createHmac } = require('node:crypto')
const { readFileSync } = require('node:fs')
const vectors = JSON.parse(readFileSync('schema/v0/webhook-signature-vectors.json', 'utf8'))
for (const c of vectors.cases) {
  const v1 = createHmac('sha256', c.secret).update(`${c.timestamp}.${c.body}`).digest('hex')
  console.log(`t=${c.timestamp},v1=${v1}` === c.signature, c.name)
}
```

Every line prints `true`. In Go, `conformance.Sign` and `conformance.VerifySignature` implement the
scheme, as does the Go client's `openworkout.VerifySignature`; the TypeScript client has
`verifySignature`. [`partner-app/src/receiver.ts`](../partner-app/src/receiver.ts) is a receiver to
copy.

## 16. The MCP profile

[`mcp/tools.json`](../mcp/tools.json) lists the tools an assistant uses over a log: names,
descriptions, input schemas, annotations, the instructions to send, and the error codes. Each
tool's `_meta` names its profile under `dev.openworkout/profile`:

- `core`: `log_workout`, `add_sets`, `update_entry`, `delete_entry`, `undo`, `get_workouts`,
  `get_personal_records`, `get_progress` and `search_exercises`. None needs an LLM. A compatible
  server offers all of them, with these shapes.
- `lifterclub`: the reference server's extensions (`log_text`, the profile, programs,
  `match_exercises`). Others may copy them but needn't.

[`mcp/README.md`](../mcp/README.md) says what a compatible server keeps to (no invented numbers,
honest matches, idempotent writes, calm mode, read-only access, actionable errors) and what an AI
client keeps to. Read it before building either.

A tool call over Streamable HTTP, against the template server:

```bash
curl -s http://localhost:8080/ \
  -H 'Authorization: Bearer dev-token' \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"log_workout","arguments":{"idempotency_key":"guide-mcp-1","exercises":[{"name":"bench press","sets":[{"reps":5,"load":80,"unit":"kg"}]},{"name":"zercher squat","sets":[{"reps":8}]}]}}}'
```

The result has two text blocks, the summary for the assistant to tell the person and then the
result as JSON, plus `structuredContent`:

```
Logged: Barbell Bench Press 5 @ 80 kg, "zercher squat" 8. Some exercises need confirming.
```

Failures are tool errors (`isError`) whose text starts with a profile code, a colon and a space,
such as `undo_unavailable: …`. A read-only credential sees and may call only the tools annotated
`readOnlyHint`.

To connect an assistant, see the [MCP quickstart](../quickstarts/mcp.md).

## 17. Building a compatible server

Start from [`mcp/template`](../mcp/template): Go, the official MCP SDK, the API core at `/v1` and the
MCP core at `/`, over an in-memory store. Its README lists what to change to make it real: storage,
OAuth, webhook delivery, limits and the full catalogue.

### Checking it

`ows conformance` checks the REST API with a token carrying `workouts:read` and `workouts:write`.
`ows conformance mcp` checks the MCP tools with a writing credential and a read-only one for the
same person. Both log one workout and delete it, so use a test account. Against the template
server:

```bash
OWS_TOKEN=dev-token go run ./cmd/ows conformance http://localhost:8080
OWS_TOKEN=dev-token OWS_READ_TOKEN=read-token go run ./cmd/ows conformance mcp http://localhost:8080/
```

```
PASS discovery
PASS create
...
PASS webhooks
PASS document
13 checks: 13 passed, 0 failed, 0 skipped

PASS discovery
PASS tools
...
PASS delete
9 checks: 9 passed, 0 failed, 0 skipped
```

The [README](../README.md#check-a-server) lists what each check asks. Outside a checkout, install
the command with `go install github.com/openexercise/open-workout/cmd/ows@latest`.

### In your own tests

The `conformance` package runs the same checks from Go. This test file ran against the template
server:

```go
package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	openworkout "github.com/openexercise/open-workout/clients/go"
	"github.com/openexercise/open-workout/conformance"
)

func TestConformance(t *testing.T) {
	base := os.Getenv("OWS_BASE_URL")
	if base == "" {
		t.Skip("OWS_BASE_URL not set")
	}
	report, err := conformance.Run(context.Background(), conformance.Config{
		BaseURL: base,
		Token:   os.Getenv("OWS_TOKEN"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range report.Results {
		if r.Status != conformance.Pass {
			t.Errorf("%s: %s %s", r.Name, r.Status, r.Detail)
		}
	}
}

func TestSignatureVectors(t *testing.T) {
	vectors, err := conformance.SignatureVectors()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		at := time.Unix(v.Timestamp, 0)
		if got := conformance.Sign(v.Secret, at, []byte(v.Body)); got != v.Signature {
			t.Errorf("%s: signed %s, want %s", v.Name, got, v.Signature)
		}
		if !openworkout.VerifySignature(v.Secret, v.Signature, []byte(v.Body), at, 5*time.Minute) {
			t.Errorf("%s: the client refused a good signature", v.Name)
		}
	}
}

func TestProgression(t *testing.T) {
	report, err := conformance.CheckVectors(context.Background(),
		func(ctx context.Context, input json.RawMessage) (conformance.VectorTarget, error) {
			return conformance.VectorTarget{NeedsStart: true}, nil // your evaluator here
		})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed() {
		t.Log("the evaluator disagrees with the published cases")
	}
}
```

`conformance.RunMCP` with a `conformance.MCPConfig` does the same for the MCP tools. Lifterclub, the
reference server, runs `Run`, `RunMCP` and `CheckVectors` in its CI.

A server that passes may say so, and add itself to the
[conformant servers](../README.md#conformant-servers) list by pull request.

## 18. The clients

Both clients are thin. They send the token on every request and an `Idempotency-Key` on every POST,
turn problems into errors (with `Retry-After` when rate limited), and page through the workout
list and the change feed. Getting the token is the app's job.

Go (`go get github.com/openexercise/open-workout/clients/go`), against the template server:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	openworkout "github.com/openexercise/open-workout/clients/go"
)

func main() {
	ctx := context.Background()
	c := openworkout.New(os.Getenv("OWS_BASE_URL"), os.Getenv("OWS_TOKEN"))

	reps, load := 5, 100.0
	logged, err := c.LogWorkout(ctx, openworkout.WorkoutInput{
		ExternalID: "myapp-workout-7",
		Title:      "Lower",
		Exercises: []openworkout.ExerciseInput{{
			ExerciseID: "oec:barbell-back-squat",
			Sets:       []openworkout.SetInput{{Reps: &reps, Load: &load, Unit: openworkout.Kg}},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(logged.Workout.ID, len(logged.NeedsConfirmation))

	for ch, err := range c.Changes(ctx, openworkout.ChangesOptions{UpdatedSince: time.Now().Add(-time.Hour), IncludeDeleted: true}) {
		if err != nil {
			log.Fatal(err)
		}
		if ch.Deleted != nil {
			fmt.Println("deleted", ch.Deleted.ID)
			continue
		}
		fmt.Println("changed", ch.Workout.ID, ch.UpdatedAt().Format(time.RFC3339))
	}
}
```

`c.LogDocument(ctx, workout)` posts an OWS `Workout` as it is. Errors are `*openworkout.Error`;
`openworkout.HasCode(err, openworkout.CodeRateLimited)` tests the code.

TypeScript (`npm install @openexercise/open-workout-client`), as an ES module on Node, against the
template server:

```js
import { changes, createOpenWorkoutClient, unwrap } from '@openexercise/open-workout-client'

const api = createOpenWorkoutClient({ baseUrl: process.env.OWS_BASE_URL, token: () => process.env.OWS_TOKEN })

const logged = unwrap(
  await api.POST('/v1/workouts', {
    body: { external_id: 'my-app-1', exercises: [{ exercise_id: 'oec:barbell-bench-press', sets: [{ reps: 5, load: 80, unit: 'kg' }] }] },
  }),
)
console.log('logged', logged.workout.id)

const since = new Date(Date.now() - 60 * 60 * 1000)
for await (const change of changes(api, { updatedSince: since, includeDeleted: true })) {
  if (change.kind === 'workout') console.log('changed', change.workout.id)
  else console.log('deleted', change.tombstone.id)
}
```

Each client's README has the rest: [Go](../clients/go/README.md),
[TypeScript](../clients/ts/README.md). The [TypeScript](../quickstarts/typescript.md) and
[Go](../quickstarts/go.md) quickstarts add sign-in and webhooks.
