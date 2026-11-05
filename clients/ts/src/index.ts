// A thin client for any Open Workout server: openapi-fetch over the types of the Open Workout API
// core, with the bearer token and an Idempotency-Key on every POST, plus helpers for errors,
// paging and webhook signatures. Lifterclub's routes beyond the core are in
// `@openexercise/open-workout-client/lifterclub`.
import createClient, { type Client } from 'openapi-fetch'

import type { components, paths } from './core.js'
import { authorizing, fetchOptions, pagesOfChanges, pagesOfWorkouts, unwrap, type Change, type ChangeOptions, type ClientOptions } from './shared.js'

export type { components, paths }
export {
  ApiError,
  isBearerError,
  isProblem,
  lifterclubSignatureHeader,
  retryAfter,
  signatureHeader,
  unwrap,
  verifySignature,
  type Change,
  type ChangeOptions,
  type ClientOptions,
} from './shared.js'

export type Workout = components['schemas']['Workout']
export type WorkoutInput = components['schemas']['WorkoutInput']
export type WorkoutList = components['schemas']['WorkoutList']
export type Tombstone = components['schemas']['Tombstone']
export type Logged = components['schemas']['Logged']
export type Deleted = components['schemas']['Deleted']
export type Problem = components['schemas']['Problem']
export type BearerError = components['schemas']['BearerError']
export type ExerciseMatch = components['schemas']['ExerciseMatch']
export type Webhook = components['schemas']['Webhook']
export type WebhookInput = components['schemas']['WebhookInput']

export type ListWorkoutsQuery = NonNullable<paths['/v1/workouts']['get']['parameters']['query']>

/** The openapi-fetch client over the core's paths: `api.GET('/v1/workouts', { params: { query } })`. */
export type OpenWorkoutClient = Client<paths>

/**
 * Makes a client for the Open Workout server at `baseUrl` that sends the token on every request
 * and an `Idempotency-Key` on every POST that has none, so a retry after a lost reply never
 * writes twice. Pass your own key in `headers` to retry across restarts.
 */
export function createOpenWorkoutClient(options: ClientOptions & { baseUrl: string }): OpenWorkoutClient {
  const client = createClient<paths>(fetchOptions(options, options.baseUrl))
  client.use(authorizing(options))
  return client
}

/** Every workout matching the query, newest first, following `next_cursor` page by page. */
export function workouts(
  api: OpenWorkoutClient,
  query: Omit<ListWorkoutsQuery, 'cursor' | 'updated_since' | 'include_deleted'> = {},
): AsyncGenerator<Workout> {
  return pagesOfWorkouts(async (q) => unwrap(await api.GET('/v1/workouts', { params: { query: q } })), query)
}

/**
 * The change feed: every workout whose record changed at or after `updatedSince`, oldest change
 * first, and with `includeDeleted` the workouts deleted since then. Keep the latest `updated_at`
 * seen and pass it back as the next `updatedSince`.
 */
export function changes(api: OpenWorkoutClient, options: ChangeOptions): AsyncGenerator<Change> {
  return pagesOfChanges(async (q) => unwrap(await api.GET('/v1/workouts', { params: { query: q } })), options)
}
