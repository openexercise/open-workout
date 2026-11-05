// Keeping a copy of a person's log in step with the server, through the change feed: every
// workout changed since the last sync, oldest change first, and the deleted ones as tombstones.

import { changes, type OpenWorkoutClient, type Workout } from '@openexercise/open-workout-client'

export class Mirror {
  readonly workouts = new Map<string, Workout>()
  /** The latest updated_at seen: the next sync starts here. Persist it in a real app. */
  since: string

  constructor(since: Date = new Date(0)) {
    this.since = since.toISOString()
  }

  /** Applies every change since the last sync, and returns how many it applied. */
  async sync(api: OpenWorkoutClient): Promise<number> {
    let applied = 0
    for await (const change of changes(api, { updatedSince: this.since, includeDeleted: true })) {
      if (change.kind === 'workout') {
        const w = change.workout
        if (w.id) {
          this.workouts.set(w.id, w)
        }
        this.advance(w.updated_at)
      } else {
        this.workouts.delete(change.tombstone.id)
        this.advance(change.tombstone.updated_at)
      }
      applied++
    }
    return applied
  }

  // The feed includes changes at the time asked for, so a change seen twice is applied twice,
  // which is harmless: the copy ends up the same.
  private advance(at: string | undefined): void {
    if (at && at > this.since) {
      this.since = at
    }
  }
}
