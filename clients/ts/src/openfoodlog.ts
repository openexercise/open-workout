// A thin client for any Open Food Log server: openapi-fetch over the types of the Open Food Log
// API core, with the bearer token and an Idempotency-Key on every POST, plus helpers for errors,
// the meals change feed and webhook signatures. Imported as
// `@openexercise/open-workout-client/openfoodlog`; Lifterclub's whole API, both cores included,
// is `@openexercise/open-workout-client/lifterclub`.
import createClient, { type Client } from 'openapi-fetch'

import type { components, paths } from './ofl-core.js'
import { authorizing, fetchOptions, unwrap, type ClientOptions } from './shared.js'

export type { components, paths }
export { ApiError, isBearerError, isProblem, retryAfter, unwrap, verifySignature, type ClientOptions } from './shared.js'

export type Meal = components['schemas']['Meal']
export type MealInput = components['schemas']['MealInput']
export type MealWritten = components['schemas']['MealWritten']
export type MealList = components['schemas']['MealList']
export type MealTombstone = components['schemas']['MealTombstone']
export type MealPatch = components['schemas']['MealPatch']
export type MealItemPatch = components['schemas']['MealItemPatch']
export type Food = components['schemas']['Food']
export type FoodMatch = components['schemas']['FoodMatch']
export type FoodRequest = components['schemas']['FoodRequest']
export type Webhook = components['schemas']['MealWebhook']
export type WebhookInput = components['schemas']['MealWebhookInput']
export type Log = components['schemas']['OFLExport']

/** The header a webhook delivery is signed in; check it with `verifySignature`. */
export const signatureHeader = 'OFL-Signature'

/** The openapi-fetch client over the core's paths: `api.GET('/v1/meals', { params: { query } })`. */
export type OpenFoodLogClient = Client<paths>

/**
 * Makes a client for the Open Food Log server at `baseUrl` that sends the token on every request
 * and an `Idempotency-Key` on every POST that has none, so a retry after a lost reply never
 * writes twice. Pass your own key in `headers` to retry across restarts.
 */
export function createOpenFoodLogClient(options: ClientOptions & { baseUrl: string }): OpenFoodLogClient {
  const client = createClient<paths>(fetchOptions(options, options.baseUrl))
  client.use(authorizing(options))
  return client
}

/** One change in the meals feed: a meal as it is now, or a tombstone for one deleted. */
export type MealChange = { kind: 'meal'; meal: Meal } | { kind: 'deleted'; tombstone: MealTombstone }

/** The options of the meals change feed. */
export interface MealChangeOptions {
  updatedSince: string | Date
  includeDeleted?: boolean
  limit?: number
}

/**
 * The change feed: every meal whose record changed at or after `updatedSince`, oldest change
 * first, and with `includeDeleted` the meals deleted since then. Keep the latest `updated_at`
 * seen and pass it back as the next `updatedSince`.
 */
export async function* mealChanges(api: OpenFoodLogClient, options: MealChangeOptions): AsyncGenerator<MealChange> {
  const since = options.updatedSince instanceof Date ? options.updatedSince.toISOString() : options.updatedSince
  let cursor: string | undefined
  do {
    const page: MealList = unwrap(
      await api.GET('/v1/meals', {
        params: { query: { updated_since: since, include_deleted: options.includeDeleted, limit: options.limit, cursor } },
      }),
    )
    for (const meal of page.meals) {
      yield { kind: 'meal', meal }
    }
    for (const tombstone of page.deleted ?? []) {
      yield { kind: 'deleted', tombstone }
    }
    cursor = page.truncated ? page.next_cursor : undefined
  } while (cursor)
}
