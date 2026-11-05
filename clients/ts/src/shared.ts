// What the Open Workout client and the Lifterclub client share: the token and Idempotency-Key
// middleware, errors, paging and webhook signatures. Types come from the core document, whose
// shapes Lifterclub's document repeats.
import type { Middleware } from 'openapi-fetch'

import type { components } from './core.js'

type Schemas = components['schemas']
type Problem = Schemas['Problem']
type BearerError = Schemas['BearerError']
type Workout = Schemas['Workout']
type WorkoutList = Schemas['WorkoutList']
type Tombstone = Schemas['Tombstone']

export interface ClientOptions {
  /** The server's origin, such as `https://api.lifterclub.com`. */
  baseUrl?: string
  /** The access token, or a function giving the current one, called before each request. */
  token: string | (() => string | Promise<string>)
  /** Replaces `globalThis.fetch`, for tests and other runtimes. */
  fetch?: (request: Request) => Promise<Response>
  /** Makes the `Idempotency-Key` for a POST that has none. Default `crypto.randomUUID`. */
  idempotencyKey?: () => string
}

/** The header a webhook delivery is signed in. */
export const signatureHeader = 'OWS-Signature'

/** The same signature under its earlier name, which Lifterclub also sends until OWS v1. */
export const lifterclubSignatureHeader = 'Lifterclub-Signature'

/** The fetch options both clients pass to openapi-fetch. */
export function fetchOptions(options: ClientOptions, baseUrl: string) {
  return {
    baseUrl: baseUrl.replace(/\/$/, ''),
    fetch: options.fetch ?? ((request: Request) => globalThis.fetch(request)),
  }
}

/** Sends the token on every request, and an `Idempotency-Key` on every POST that has none. */
export function authorizing(options: ClientOptions): Middleware {
  const newKey = options.idempotencyKey ?? (() => crypto.randomUUID())
  return {
    async onRequest({ request }) {
      const token = typeof options.token === 'function' ? await options.token() : options.token
      request.headers.set('Authorization', `Bearer ${token}`)
      if (request.method === 'POST' && !request.headers.has('Idempotency-Key')) {
        request.headers.set('Idempotency-Key', newKey())
      }
      return request
    },
  }
}

/** A failed call: the problem's `code` (or the bearer error's `error`) and status, to branch on. */
export class ApiError extends Error {
  readonly status: number
  /** `rate_limited`, `not_found`, `invalid_token`… */
  readonly code: string
  readonly detail: string | undefined
  /** The RFC 9457 problem, when the server sent one. */
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

/** The query of one page of `GET /v1/workouts`. */
export interface PageQuery {
  from?: string
  to?: string
  exercise?: string
  limit?: number
  cursor?: string
  updated_since?: string
  include_deleted?: boolean
}

/** Gets one page of `GET /v1/workouts`, from whichever client. */
export type PageGetter = (query: PageQuery) => Promise<WorkoutList>

/** Every workout a page getter returns, newest first, following `next_cursor`. */
export async function* pagesOfWorkouts(get: PageGetter, query: PageQuery): AsyncGenerator<Workout> {
  let cursor: string | undefined
  do {
    const page = await get({ ...query, cursor })
    yield* page.workouts
    cursor = page.truncated ? page.next_cursor : undefined
  } while (cursor)
}

/** One change in the feed: a workout as it is now, or a tombstone for one deleted. */
export type Change = { kind: 'workout'; workout: Workout } | { kind: 'deleted'; tombstone: Tombstone }

/** The options of the change feed. */
export interface ChangeOptions {
  updatedSince: string | Date
  includeDeleted?: boolean
  limit?: number
}

/** Every change a page getter returns, oldest first, following `next_cursor`. */
export async function* pagesOfChanges(get: PageGetter, options: ChangeOptions): AsyncGenerator<Change> {
  const since =
    options.updatedSince instanceof Date ? options.updatedSince.toISOString() : options.updatedSince
  let cursor: string | undefined
  do {
    const page = await get({ updated_since: since, include_deleted: options.includeDeleted, limit: options.limit, cursor })
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
 * Checks a webhook delivery's `OWS-Signature` header (`t=<unix seconds>,v1=<hex HMAC-SHA256 of
 * "<t>.<body>">`) against the raw body with the subscription's secret, and refuses a timestamp
 * further than `toleranceSeconds` from `now`.
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
