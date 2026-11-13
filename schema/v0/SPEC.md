# Open Workout Schema v0

Draft. The JSON Schemas in this folder are normative for shape; this document says what the members
mean and how documents change. MUST, SHOULD and MAY are as in RFC 2119.

## 1. Why this exists

A workout log should belong to the person, not the app that recorded it. OWS is one shape for that
log, so any app, assistant or server can write it and read it back, and a person can take it
elsewhere whole.

## 2. Documents

| Document | Schema | What it is |
| --- | --- | --- |
| Workout | [`workout.json`](workout.json) | One session: exercise entries with their sets, when it happened, and where the record came from |
| Routine | [`routine.json`](routine.json) | A person's own plan for a session: targets, never performed work |
| Program | [`program.json`](program.json) | What a program prescribes, week by week: rules for loads, not weights |
| Log | [`log.json`](log.json) | A whole log as one file: workouts, routines and deleted workouts |
| Vectors | [`progression-vectors.json`](progression-vectors.json), [`webhook-signature-vectors.json`](webhook-signature-vectors.json) | Worked cases every implementation must agree on |

Documents are UTF-8 JSON, valid against JSON Schema draft 2020-12, and MAY say which version they
were written to in `ows_version` (section 8). Producers MAY add `x-<vendor>-<name>` members to a
workout, an entry, a set, a routine or a routine row, and to a program, a day or a slot. Consumers
MUST ignore `x-` members they don't understand.

## 3. Identity

- **IDs are opaque strings.** A server assigns a document's `id`, and its entries' and sets' `id`s;
  a writer never chooses them. A reader MUST NOT parse them.
- **Exercises are named by Open Exercise Catalogue IDs** (`oec:<slug>`, https://openexercise.org).
  The catalogue renames by listing an old ID in a record's `previous_ids`. A reader keeping workouts
  under an old ID SHOULD move them to the new one, and MUST still resolve the old one.
- **Other prefixes** name exercises outside the catalogue: `usr:` is a person's own, and a server's
  own prefix (the reference server's `lc:`) is one it added itself until the catalogue adopts it
  under an `oec:` ID, through `previous_ids`. A server MUST NOT mint IDs in the `oec:` namespace.
- **`source.external_id`** is the writing app's own ID for a workout. A server that stores workouts
  from several apps keys it per app: the same `external_id` from the same app is the same workout.

## 4. Workouts

- **When.** `started_at` (and `ended_at`) are instants with an offset. `date` is the workout's local
  day: `started_at` in `timezone`, the IANA zone it happened in. A reader files a workout under
  `date` wherever the reader is. When a writer knows only the day, a server works the start out,
  and lists it in `estimates` as `/started_at`: trust its day, not its hour.
- **Entries.** Each holds the person's own words for the exercise (`text`) and what it was matched
  to (`exercise`: `id`, `name`, `match`).
  - **Confirmed matches:** `catalogue` and `custom`.
  - **Unconfirmed:** `close` and `unmatched` wait for the person to confirm, and MUST NOT count toward
    records until they do.
- **Sets** have a `type`. Only `warmup` sets are left out of records.
  - **Loads** are `{value, unit}` in the unit the person gave (`kg` or `lb`); a reader converts, and
    MUST NOT store the converted number as given.
  - **Time and distance** are `duration_s` in seconds and `distance_m` in metres.
  - **Device readings:** `calories` and `heart_rate_bpm` are as a device or app reported them.
- **Estimates.** `estimates` lists JSON pointers (RFC 6901) to values that were inferred rather than
  given:
  - a set, or a value in one;
  - a set's `/load/unit` when the unit was assumed;
  - `/started_at`.

  A number not listed there was given by the person. A writer MUST NOT invent one without listing it.
- **Provenance.** `source` says where the record came from. `merged_sources` lists other apps'
  records of the same session that a server merged into this one.

## 5. Routines and programs

A routine's rows and a program's slots are **targets**: what to aim for, never what was done. A
workout logged from one records what was done. A program prescribes loads as rules (a percentage, a
progression), so the same program gives each person their own numbers; `progression-vectors.json`
holds worked cases of those rules.

## 6. The log

A log holds a person's workouts (newest first), their routines, and the workouts they deleted
(`deleted_workouts`, each with `deleted_at` and, for one merged into another, `merged_into`). A server
MAY add more of the person's record beside them, under keys this schema doesn't name; readers ignore
keys they don't know.

## 7. Servers

The REST API core (`api/core.yaml`, generated from the reference server) and the MCP tool profile
(`mcp/tools.json`) say how a server serves these documents:
- **Writing:** `Idempotency-Key` and `external_id`.
- **Mirroring:** a change feed with tombstones.
- **Webhooks:** signed in `OWS-Signature`; `webhook-signature-vectors.json` holds worked cases.

`ows conformance <base-url>` checks a server against them.

## 8. Versions and extensions

- **Naming a version.** `ows_version` names the version a document was written to, as `MAJOR.MINOR`
  (`"0.1"`). A document without one is `0.0`: the drafts before versions were numbered, dated in the
  [changelog](../CHANGELOG.md). The schemas' `$id`s stay `…/schema/v0/` for every 0.x, and always
  serve the latest 0.x.
- **A minor version only adds:** new optional members, enum values and documents. It never removes
  or renames a member, never changes a unit or what a value means, and never makes an optional member
  required. A document valid under 0.n stays valid under 0.n+1.
- **Validators** check against the schemas at their `$id`, which are the latest 0.x. A copy kept
  locally goes stale with the next minor version; refresh it rather than rejecting a newer document.
- **A reader meeting something newer** keeps the document:
  - it ignores members it doesn't know;
  - it treats an enum value it doesn't know as unknown: a set `type` it doesn't know is a working
    set, a `match` it doesn't know is unconfirmed;
  - it MUST NOT drop a workout, entry or set it can't fully read.
- **Until v1**, a change that breaks this (rare, and only to fix a mistake) is named as breaking in
  the changelog. v1 freezes when the first outside app writes OWS in production; after that, breaking
  changes need a new major version and new schema `$id`s.
- **Extensions** are members named `x-<vendor>-…` on the objects listed in section 2. `<vendor>` is
  the producer's name, registered in the open-workout README (a pull request adds one). Extensions
  that prove useful to more than one producer are proposed for the core.

## 9. Not in v0

- **Body metrics** (weight, body fat) and **readiness** (sleep, heart rate variability): they
  describe the person, not the training. Use Health Connect, HealthKit or FHIR for them, and link
  by time.
- **GPS tracks and per-second samples.** A set records totals; the raw stream stays in the file it
  came from (FIT, GPX, TCX).
- **Supersets and circuits** as structure. Entries are in the order done, which is all a reader can
  rely on for now.
