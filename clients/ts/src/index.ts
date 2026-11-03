// A thin client for the Lifterclub API: openapi-fetch over the generated types, with the
// bearer token and an Idempotency-Key on every POST, plus helpers for errors, paging and
// webhook signatures. Everything else is the API as documented.
import createClient, { type Client, type Middleware } from 'openapi-fetch'

import type { components, paths } from './v1'

export type { components, paths }

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

/** The API's origin when none is given. */
export const defaultBaseUrl = 'https://api.lifterclub.com'

export interface ClientOptions {
  /** The API's origin. Default `https://api.lifterclub.com`. */
  baseUrl?: string
  /** The access token (`lca_…`), or a function giving the current one, called before each request. */
  token: string | (() => string | Promise<string>)
  /** Replaces `globalThis.fetch`, for tests and other runtimes. */
  fetch?: (request: Request) => Promise<Response>
  /** Makes the `Idempotency-Key` for a POST that has none. Default `crypto.randomUUID`. */
  idempotencyKey?: () => string
}

/** The openapi-fetch client over the API's paths: `api.GET('/v1/workouts', { params: { query } })`. */
export type LifterclubClient = Client<paths>

/**
 * Makes a client that sends the token on every request and an `Idempotency-Key` on every POST
 * that has none, so a retry after a lost reply never writes twice. Pass your own key in
 * `headers` to retry across restarts.
 */
export function createLifterclubClient(options: ClientOptions): LifterclubClient {
  const newKey = options.idempotencyKey ?? (() => crypto.randomUUID())
  const client = createClient<paths>({
    baseUrl: (options.baseUrl ?? defaultBaseUrl).replace(/\/$/, ''),
    fetch: options.fetch ?? ((request) => globalThis.fetch(request)),
  })
  const authorize: Middleware = {
    async onRequest({ request }) {
      const token = typeof options.token === 'function' ? await options.token() : options.token
      request.headers.set('Authorization', `Bearer ${token}`)
      if (request.method === 'POST' && !request.headers.has('Idempotency-Key')) {
        request.headers.set('Idempotency-Key', newKey())
      }
      return request
    },
  }
  client.use(authorize)
  return client
}

/** A failed call: the problem's `code` (or the bearer error's `error`) and status, to branch on. */
export class ApiError extends Error {
  readonly status: number
  /** `rate_limited`, `not_found`, `invalid_token`… */
  readonly code: string
  readonly detail: string | undefined
  /** The RFC 9457 problem, when the API sent one. */
  readonly problem: Problem | undefined
  /** Seconds to wait, from `Retry-After`, when the status is 429. */
  readonly retryAfter: number | undefined
  readonly response: Response

  constructor(error: unknown, response: Response) {
    const problem = isProblem(error) ? error : undefined
    const bearer = isBearerError(error) ? error : undefined
    const code = problem?.code ?? bearer?.error ?? `http_${response.status}`
    const detail = problem?.detail ?? bearer?.error_description
    super(detail ? `${code} (${response.status}): ${detail}` : `${code} (${response.status})`)
    this.name = 'ApiError'
    this.status = response.status
    this.code = code
    this.detail = detail
    this.problem = problem
    this.retryAfter = retryAfter(response)
    this.response = response
  }
}

/** Returns `data`, or throws an `ApiError` for a failed call. */
export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
  if (result.error !== undefined || result.data === undefined) {
    throw new ApiError(result.error, result.response)
  }
  return result.data
}

export function isProblem(error: unknown): error is Problem {
  return typeof error === 'object' && error !== null && typeof (error as Problem).code === 'string'
}

export function isBearerError(error: unknown): error is BearerError {
  return typeof error === 'object' && error !== null && typeof (error as BearerError).error === 'string'
}

/** Seconds to wait before retrying, from a 429's `Retry-After`; undefined otherwise. */
export function retryAfter(response: Response): number | undefined {
  if (response.status !== 429) {
    return undefined
  }
  const header = response.headers.get('Retry-After')
  const seconds = header === null ? NaN : Number(header)
  return Number.isFinite(seconds) ? seconds : undefined
}

/** Every workout matching the query, newest first, following `next_cursor` page by page. */
export async function* workouts(
  api: LifterclubClient,
  query: Omit<ListWorkoutsQuery, 'cursor' | 'updated_since' | 'include_deleted'> = {},
): AsyncGenerator<Workout> {
  let cursor: string | undefined
  do {
    const page = unwrap(await api.GET('/v1/workouts', { params: { query: { ...query, cursor } } }))
    yield* page.workouts
    cursor = page.truncated ? page.next_cursor : undefined
  } while (cursor)
}

/** One change in the feed: a workout as it is now, or a tombstone for one deleted. */
export type Change = { kind: 'workout'; workout: Workout } | { kind: 'deleted'; tombstone: Tombstone }

/**
 * The change feed: every workout whose record changed at or after `updatedSince`, oldest change
 * first, and with `includeDeleted` the workouts deleted since then. Keep the latest `updated_at`
 * seen and pass it back as the next `updatedSince`.
 */
export async function* changes(
  api: LifterclubClient,
  options: { updatedSince: string | Date; includeDeleted?: boolean; limit?: number },
): AsyncGenerator<Change> {
  const since =
    options.updatedSince instanceof Date ? options.updatedSince.toISOString() : options.updatedSince
  let cursor: string | undefined
  do {
    const page = unwrap(
      await api.GET('/v1/workouts', {
        params: {
          query: { updated_since: since, include_deleted: options.includeDeleted, limit: options.limit, cursor },
        },
      }),
    )
    for (const workout of page.workouts) {
      yield { kind: 'workout', workout }
    }
    for (const tombstone of page.deleted ?? []) {
      yield { kind: 'deleted', tombstone }
    }
    cursor = page.truncated ? page.next_cursor : undefined
  } while (cursor)
}

/**
 * Checks a webhook delivery's `Lifterclub-Signature` header (`t=<unix seconds>,v1=<hex
 * HMAC-SHA256 of "<t>.<body>">`) against the raw body with the subscription's secret, and
 * refuses a timestamp further than `toleranceSeconds` from `now`.
 */
export async function verifySignature(
  secret: string,
  header: string | null | undefined,
  body: string,
  now: Date = new Date(),
  toleranceSeconds = 300,
): Promise<boolean> {
  const parts = Object.fromEntries((header ?? '').split(',').map((p) => p.split('=', 2) as [string, string]))
  const t = Number(parts.t)
  const v1 = parts.v1
  if (!Number.isInteger(t) || !v1 || Math.abs(now.getTime() / 1000 - t) > toleranceSeconds) {
    return false
  }
  const key = await crypto.subtle.importKey('raw', new TextEncoder().encode(secret), { name: 'HMAC', hash: 'SHA-256' }, false, ['sign'])
  const mac = await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(`${t}.${body}`))
  const expected = Array.from(new Uint8Array(mac), (b) => b.toString(16).padStart(2, '0')).join('')
  return timingSafeEqual(expected, v1.toLowerCase())
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) {
    return false
  }
  let diff = 0
  for (let i = 0; i < a.length; i++) {
    diff |= a.charCodeAt(i) ^ b.charCodeAt(i)
  }
  return diff === 0
}
