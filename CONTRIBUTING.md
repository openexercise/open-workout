# Contributing

Open an issue. Until v1 the schema and the tool profile are published here from the reference implementation, so a change starts as an issue or proposal in this repository, is agreed here, and then lands in the next published version with a changelog entry.

## Proposing a change

1. Say what real workouts or clients the change would serve, with an example document.
2. Say whether it is additive (a new optional field) or breaking (a renamed or removed field, a changed meaning). Before v1 both are possible; after v1 only additive changes land without a new major version.
3. Prefer recording what a person stated. A field whose value a server would have to invent does not belong in the schema; inferred values are listed in `estimates` instead.

## Rules for the schema

- Every number has a stated unit, and loads keep the unit the person gave.
- Provenance (`source`, `external_id`, `merged_sources`) travels with the record.
- An exercise is identified by its Open Exercise ID; names are what the person wrote.
