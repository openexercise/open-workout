// The client for Lifterclub's API: the Open Workout core plus Lifterclub's extensions (sentence
// parsing, records and progress, the profile, routines, programs, imports, partners), typed from
// Lifterclub's full document. Same helpers as the core client.
import createClient, { type Client } from 'openapi-fetch'

import type { components, paths } from './lifterclub-api.js'
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
export type Me = components['schemas']['Me']
export type Profile = components['schemas']['Profile']
export type CurrentRecord = components['schemas']['CurrentRecord']
export type Progress = components['schemas']['Progress']
export type ExerciseMatch = components['schemas']['ExerciseMatch']
export type LastSets = components['schemas']['LastSets']
export type Webhook = components['schemas']['Webhook']
export type WebhookInput = components['schemas']['WebhookInput']

export type ListWorkoutsQuery = NonNullable<paths['/v1/workouts']['get']['parameters']['query']>

/** Lifterclub's API origin, the default. */
export const defaultBaseUrl = 'https://api.lifterclub.com'

/** The openapi-fetch client over Lifterclub's paths. */
export type LifterclubClient = Client<paths>

/**
 * Makes a client for Lifterclub's API (`baseUrl` defaults to `https://api.lifterclub.com`), with
 * the token on every request and an `Idempotency-Key` on every POST that has none.
 */
export function createLifterclubClient(options: ClientOptions): LifterclubClient {
  const client = createClient<paths>(fetchOptions(options, options.baseUrl ?? defaultBaseUrl))
  client.use(authorizing(options))
  return client
}

/** Every workout matching the query, newest first, following `next_cursor` page by page. */
export function workouts(
  api: LifterclubClient,
  query: Omit<ListWorkoutsQuery, 'cursor' | 'updated_since' | 'include_deleted'> = {},
): AsyncGenerator<Workout> {
  return pagesOfWorkouts(async (q) => unwrap(await api.GET('/v1/workouts', { params: { query: q } })), query)
}

/** The change feed, oldest change first; see the core client's `changes`. */
export function changes(api: LifterclubClient, options: ChangeOptions): AsyncGenerator<Change> {
  return pagesOfChanges(async (q) => unwrap(await api.GET('/v1/workouts', { params: { query: q } })), options)
}
