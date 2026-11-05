# Open Workout Schema: changes

`v0` is a draft: it may change, and every change is listed here. `v1` freezes when the first
outside app writes OWS in production (`docs/platform.md`); after that changes are additive, through
RFCs in `openexercise/open-workout`.

## v0

- 2026-11-04: Three more progression shapes in `program.json`, all additive. A `linear` rule may
  carry `stages`, with `on_fail.stage_after_drops`: the drop that makes that many at one set scheme
  also moves on to the next (StrongLifts' 5x5 → 3x5 → 3x3). A new load, `percent_of_slot`
  (`value`, `day`, optional `of`), is a percentage of another slot's current target (Texas Method's
  volume and recovery days). A `linear` increment may be a percentage (`unit: "%"`) of the load or
  training max it adds to (Madcow's 2.5% a week). 11 more vectors; a `percent_of_slot` case gives
  `reference`, the target of the slot it reads.
- 2026-11-04: Three additions to `program.json` for programs that ramp through percentages of
  training maxes (nSuns-style): a set group may carry its own `percent` of the training max on a
  `percent_training_max` slot whose load has no value, so one slot holds a lift's whole ramp; a
  `percent_training_max` load may name, in `of`, the lift whose training max it works from (a front
  squat from the back squat's), and such a slot carries no rule; and `increment_table`, or `linear`
  with success `amrap`, may name in `amrap_set` which planned set it reads, when it isn't the last
  AMRAP set. Vectors added, with `want.loads` for each group's weight. Additive.
- 2026-11-04: Missing data holds for every progression rule: a session with reps unknown on a set
  the rule reads (none missing, none logged short) is neither done nor short, so the load, stage
  and training max stay. Before, `all_sets` counted such sets as done. Vectors updated and added.
- 2026-11-04: `linear` and `increment_table` may go on a `percent_training_max` slot, where they
  move the lift's training max: replayed from the person's training max through the sessions
  logged on the slots carrying a rule, with `on_fail` taking its drop off the training max. Covers
  5/3/1-style cycles (a rule on the week's top set) and nSuns-style tables. Additive.
- 2026-11-04: `variations` progression rule in `program.json`, on a slot whose load is `choice`: a
  `ladder` of Open Exercise IDs, easiest first, including the slot's own. The step to do is the
  last one logged, or the next when every planned set reached `reps` (or `hold_s` on timed sets).
  Any step counts as the slot. For bodyweight progressions; additive.
- 2026-11-04: `stages` progression rule in `program.json`: the slot's sets and then each scheme in
  `stages`. A short session moves to the next stage at the same load, every set done adds
  `increment`, and a short session on the last stage starts again from `restart` (`from` the last
  load or the last first-stage success, less `drop_percent`, plus `add`). `success` may be
  `total_reps`, where a set that went further makes up for a short one. The rule text now also
  says which sessions count for a slot: those logged on a day whose slot for the exercise has the
  same load and rule, so one lift can progress separately as a heavy and a light slot. Additive.
- 2026-11-04: Progression rules in `program.json`. A slot's `load` now says only where the load
  comes from; the new load type `last` replaces `last_plus`. A slot may carry a `progression` rule
  saying how the load moves: `linear` (an `increment` on success, with success as `all_sets`,
  `total_reps` or `amrap`), `double` (a rep range, then an increment) or `increment_table` (the
  AMRAP set's reps pick an increment from `bands`), each with an optional `on_fail` (`after_misses`,
  `drop_percent`, moved from the load). Breaking: `last_plus` with `value` and `unit` is now `last`
  with `progression: {type: linear, increment: {value, unit}}`. Readers should treat an old
  document the same way. Research and the rules still to come are in Lifterclub's
  `docs/progressions.md`.
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
