# Open Workout Schema: changes

`v0` is a draft: it may change, and every change is listed here. `v1` freezes when the first
outside app writes OWS in production (`docs/platform.md`); after that changes are additive, through
RFCs in `openexercise/open-workout`.

## v0

- 2026-11-03: `calories` and `heart_rate_bpm` added to a set: kilocalories as a machine or app
  reported them, and the average heart rate over the set. Both optional, named after the Open
  Exercise metrics `calories` and `heart-rate`, for cardio logged by other apps and watches.
- 2026-11-02: `updated_at` added to a workout: when the stored record last changed, by the server's
  clock, any change to the workout, its entries, sets or program label. Optional; for mirroring a
  log through a change feed.
- 2026-11-01: Published alongside the OWS MCP tool profile (`mcp/tools.json`). Exercises are
  identified by Open Exercise IDs (`https://openexercise.org/schema/v0/exercise.json`). No schema
  change.
- 2026-10-30: `program.json` added: a program as a library publishes it. A workout may carry a
  `program` label naming the program day it was part of; `listed: false` means the program was
  withdrawn and says nothing more.
- 2026-10-29: `merged_sources` added to a workout: other records of the same session merged into
  it, such as the same run imported from two apps.
- 2026-10-29: `v0` introduced: a workout with its `source`, exercise entries with their `exercise`
  match (`catalogue`, `close`, `custom` or `unmatched`) and the user's own `text`, sets, and every
  inferred value listed in `estimates` by JSON pointer.
