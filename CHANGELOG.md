# Changelog

The running record of this repository: the Open Workout Schema, the MCP tool profile and its template
server, the API core, the conformance suite, and the Go and TypeScript clients. Until v1 the
schema, profile, API documents and clients are published here from Lifterclub, the reference
implementation (`scripts/publish-openworkout.sh` there).

Versions follow semantic versioning and are released per client, as git tags: `clients-ts-vX.Y.Z`
(version file `clients/ts/package.json`; the tag publishes to npm) and `clients/go/vX.Y.Z` (the Go
module in `clients/go`). The schema itself is the v0 draft; its own changes are in
[`schema/CHANGELOG.md`](schema/CHANGELOG.md). `git log` has the detail.

## Still to do

Known gaps and deferred work, removed when it ships (and recorded under Unreleased).

- **v1:** freezes when the first application outside Lifterclub writes the schema in production;
  after that changes are additive, through RFCs ([README.md](README.md),
  [`schema/CHANGELOG.md`](schema/CHANGELOG.md)).

## Unreleased

- **MCP profile:** `log_workout`'s `exercise_id` example names the catalogue's
  `oec:barbell-bench-press` (it said the renamed `oec:bench-press`). Conformance compares input
  schemas with descriptions, so a server checked against this profile needs the new wording.
- **API reference (`api/v1.yaml`) and the TypeScript client's Lifterclub types:** the export holds
  the person's own `exercises`, `exercise_phrases` and `food_phrases`.
- **Contributor and developer guides:** `CONTRIBUTING.md` now covers setup, which paths are
  published from Lifterclub until v1, how a schema change is proposed here and lands there, adding
  conformance checks, the checks to run and releases; `docs/developer-guide.md` walks through the
  documents member by member, the API core, webhooks, the MCP profile, conformance and the clients.
  `examples/lower-day.json` shows a local day, set types, an unmatched entry, cardio and an estimate.
- **Schema 0.1:** versions, extension members and a prose specification (`schema/v0/SPEC.md`).
  Workouts, routines, programs and logs gain `ows_version`; workouts, entries, sets, routines, rows,
  programs, days and slots allow `x-<vendor>-…` members. The Go client's `Workout` reads
  `ows_version`. Additive ([`schema/CHANGELOG.md`](schema/CHANGELOG.md)).
- **The specification on the site:** openworkout.dev serves `SPEC.md` at `/schema/v0/spec/`, linked
  from the schema page, and Markdown tables now render.
- **Lifterclub's API changes, published:** the API core's `WorkoutInput` names `ows_version`, and
  `api/v1.yaml`, `api/ofl-core.yaml` and the TypeScript client's types catch up with recipes, saved
  meals, targets, meal photos and the import sources (no client release yet).

## clients/go/v0.2.0 and clients-ts-v0.5.0 — 2026-11-13

Both clients catch up with the schema and the API core: the workout's local day and time zone,
OWS documents written as they are, and Open Food Log in the TypeScript client. Go v0.2.0 only adds
fields and `LogDocument`; TypeScript 0.5.0 breaks core-only clients (#18).

### Added

- **Lifterclub's changes since 2026-11-05, published** (#18): TypeScript client 0.5.0 (on npm)
  with an `/openfoodlog` entry; the API cores take OWS documents as they are; the Go client's
  `SetInput.Type`, `WorkoutInput.Estimates` and `LogDocument`. **Breaking** for core-only clients
  (mightydotrun/lifterclub#140): writes, deletes and lists lose Lifterclub's own fields (`summary`,
  `new_records`, `warnings`, `totals` and others), and `Problem` loses three codes.
- `routine.json` (a person's routine: exercise rows with optional targets and a rule) and
  `log.json` (a whole log as one file); the API core's export returns a log document (#12).
- `mcp/template`: a Go MCP server for the core profile with an in-memory store, its own module (#13).
  It now also serves the API core at `/v1` (workouts, undo, change feed with tombstones, export,
  search, signed webhooks) and passes `ows conformance`, with an example partner app and quickstarts
  (#16).
- The MCP README's "What an AI client keeps to", and the reference server's extension tool
  `match_exercises` (#14).
- Conformance: `change-feed`, `tombstone` and `webhooks` REST checks, `ows conformance mcp`,
  `ows vectors` for the progression vectors, and `webhook-signature-vectors.json` with
  `conformance.Sign`/`VerifySignature` (#15).
- A workout's local `date` and IANA `timezone`; the Go client reads both and sends `timezone` (#17).

### Changed

- **Breaking:** the MCP profile splits into a core and extensions (`dev.openworkout/profile` in each
  tool's `_meta`), and `error_codes` becomes a list of `{code, profile, meaning}` (#13).
- `success: total_reps` decides short as well as done on `linear` and `increment_table` (#12).

## clients/go/v0.1.0 — 2026-11-04

The Open Workout API core and neutral clients (#11). The TypeScript client became
`@openexercise/open-workout-client` 0.4.0, published to npm by hand on 2026-11-05 (a new package
name), so it has no tag.

- **Added:** `api/core.yaml`, the routes any Open Workout server serves (workouts, sets and entries,
  undo, change feed, export, webhooks, exercise search); `api/v1.yaml` is Lifterclub's whole API.
- **Added:** the Go client as package `openworkout`, a module of its own.
- **Changed:** webhooks are signed in `OWS-Signature`, with `OWS-Event` and `OWS-Delivery`.
- **Changed:** the TypeScript client serves the core at its root and Lifterclub's API at
  `/lifterclub`.

## clients-ts-v0.3.0 — 2026-11-04

- **Added:** StrongLifts, Texas Method and Madcow progressions in `program.json`: `stages` on a
  `linear` rule, the `percent_of_slot` load and percentage increments, with 11 more vectors (#10).
- **Added:** program targets carry `start_key` in `api/v1.yaml` (#10).

## clients-ts-v0.2.0 — 2026-11-04

- **Added:** progression rules in `program.json` (`linear`, `double`, `increment_table`, `stages`,
  `variations`), training-max progressions and `progression-vectors.json` (#8).
- **Changed (breaking for v0 documents):** the `last_plus` load is now `last` with a `linear` rule
  (#8).
- **Changed:** per-app limits of 1,200 reads and 120 writes a minute (#7).
- **Added:** GOVERNANCE.md, CODE_OF_CONDUCT.md and SECURITY.md (#7).
- **Fixed:** the site serves the progression vectors as a file, not a schema (#9).

## clients-ts-v0.1.1 — 2026-11-04

The first tagged release, and everything before it.

- **Added:** Open Workout Schema v0 (workout, program), the MCP tool profile, `ows validate`,
  examples and the openworkout.dev site (2026-11-01).
- **Added:** a workout's `updated_at` (2026-11-02), and a set's `calories` and `heart_rate_bpm`
  (#1).
- **Added:** `ows conformance <base-url>` and the `conformance` package, checking a server's REST API
  (#3).
- **Added:** `api/v1.yaml` and the Go and TypeScript clients (#4); the TypeScript client released to
  npm as `@openexercise/lifterclub-client` from a `clients-ts-v*` tag, with trusted publishing (#5,
  #6).
