import { createHmac } from 'node:crypto'

import { describe, expect, it } from 'vitest'

import { ApiError, changes, createLifterclubClient, retryAfter, unwrap, verifySignature, workouts } from './index'

type Handler = (request: Request) => Response | Promise<Response>

/** A fake API: records every request and answers with the handler. */
function fakeApi(handler: Handler, options: { token?: string | (() => Promise<string>) } = {}) {
  const requests: Request[] = []
  const api = createLifterclubClient({
    baseUrl: 'https://api.test/',
    token: options.token ?? 'lca_test',
    idempotencyKey: () => 'key-1',
    fetch: async (request) => {
      requests.push(request)
      return handler(request)
    },
  })
  return { api, requests }
}

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } })
}

function problem(code: string, status: number, detail: string, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify({ type: `https://lifterclub.com/docs/api#${code}`, title: code, status, detail, code }), {
    status,
    headers: { 'Content-Type': 'application/problem+json', ...headers },
  })
}

describe('createLifterclubClient', () => {
  it('sends the bearer token and an Idempotency-Key on a POST', async () => {
    const { api, requests } = fakeApi(() => json({ workout: {}, summary: '', new_records: [], needs_confirmation: [] }, 201))
    await api.POST('/v1/workouts', { body: { exercises: [{ name: 'bench', sets: [{ reps: 5 }] }] } })
    expect(requests[0]?.url).toBe('https://api.test/v1/workouts')
    expect(requests[0]?.headers.get('Authorization')).toBe('Bearer lca_test')
    expect(requests[0]?.headers.get('Idempotency-Key')).toBe('key-1')
    expect(requests[0]?.headers.get('Content-Type')).toBe('application/json')
  })

  it('keeps a key the caller gave, and puts none on a GET', async () => {
    const { api, requests } = fakeApi(() => json({ workouts: [], totals: {}, truncated: false, summary: '' }))
    await api.POST('/v1/workouts', { body: { exercises: [] }, headers: { 'Idempotency-Key': 'mine' } })
    await api.GET('/v1/workouts')
    expect(requests[0]?.headers.get('Idempotency-Key')).toBe('mine')
    expect(requests[1]?.headers.has('Idempotency-Key')).toBe(false)
  })

  it('asks a token function before each request', async () => {
    let n = 0
    const { api, requests } = fakeApi(() => json({}), { token: async () => `lca_${++n}` })
    await api.GET('/v1/me')
    await api.GET('/v1/me')
    expect(requests.map((r) => r.headers.get('Authorization'))).toEqual(['Bearer lca_1', 'Bearer lca_2'])
  })
})

describe('unwrap', () => {
  it('returns the data of a successful call', async () => {
    const { api } = fakeApi(() => json({ id: 'u1', display_name: 'Ada' }))
    const me = unwrap(await api.GET('/v1/me'))
    expect(me.display_name).toBe('Ada')
  })

  it('throws an ApiError with the problem code, and Retry-After when rate limited', async () => {
    const { api } = fakeApi(() => problem('rate_limited', 429, 'at most 60 writes a minute per account', { 'Retry-After': '60' }))
    const result = await api.POST('/v1/workouts', { body: { exercises: [] } })
    expect(() => unwrap(result)).toThrow(ApiError)
    try {
      unwrap(result)
    } catch (err) {
      const e = err as ApiError
      expect(e.code).toBe('rate_limited')
      expect(e.status).toBe(429)
      expect(e.retryAfter).toBe(60)
      expect(e.problem?.detail).toContain('per account')
      expect(e.message).toBe('rate_limited (429): at most 60 writes a minute per account')
    }
    expect(retryAfter(result.response)).toBe(60)
  })

  it('reads a bearer error', async () => {
    const { api } = fakeApi(() => json({ error: 'invalid_token', error_description: 'expired' }, 401))
    try {
      unwrap(await api.GET('/v1/me'))
      expect.unreachable()
    } catch (err) {
      const e = err as ApiError
      expect(e.code).toBe('invalid_token')
      expect(e.status).toBe(401)
      expect(e.retryAfter).toBeUndefined()
    }
  })
})

describe('paging', () => {
  it('workouts follows next_cursor with the same filters', async () => {
    const { api, requests } = fakeApi((request) => {
      const cursor = new URL(request.url).searchParams.get('cursor')
      return cursor === null
        ? json({ workouts: [{ id: 'w1' }, { id: 'w2' }], totals: {}, truncated: true, next_cursor: 'c2', summary: '' })
        : json({ workouts: [{ id: 'w3' }], totals: {}, truncated: false, summary: '' })
    })
    const ids: string[] = []
    for await (const w of workouts(api, { from: '2026-01-01', limit: 2 })) {
      ids.push(w.id ?? '')
    }
    expect(ids).toEqual(['w1', 'w2', 'w3'])
    expect(requests.map((r) => new URL(r.url).search)).toEqual(['?from=2026-01-01&limit=2', '?from=2026-01-01&limit=2&cursor=c2'])
  })

  it('changes yields workouts then tombstones, page by page', async () => {
    const { api, requests } = fakeApi((request) => {
      const cursor = new URL(request.url).searchParams.get('cursor')
      return cursor === null
        ? json({ workouts: [{ id: 'w1' }], deleted: [{ id: 'd1', deleted_at: 't', updated_at: 't' }], totals: {}, truncated: true, next_cursor: 'c2', summary: '' })
        : json({ workouts: [{ id: 'w2' }], totals: {}, truncated: false, summary: '' })
    })
    const seen: string[] = []
    for await (const c of changes(api, { updatedSince: new Date('2026-11-01T00:00:00Z'), includeDeleted: true })) {
      seen.push(c.kind === 'workout' ? `workout:${c.workout.id}` : `deleted:${c.tombstone.id}`)
    }
    expect(seen).toEqual(['workout:w1', 'deleted:d1', 'workout:w2'])
    expect(new URL(requests[0]?.url ?? '').search).toBe('?updated_since=2026-11-01T00%3A00%3A00.000Z&include_deleted=true')
  })
})

describe('verifySignature', () => {
  const secret = 'lcw_secret'
  const body = '{"event":"workout.created","workout_id":"w1"}'
  const now = new Date('2026-11-03T12:00:00Z')
  const t = Math.floor(now.getTime() / 1000)
  const sign = (at: number) => `t=${at},v1=${createHmac('sha256', secret).update(`${at}.${body}`).digest('hex')}`

  it('accepts the server\'s signature', async () => {
    expect(await verifySignature(secret, sign(t), body, now)).toBe(true)
  })

  it('refuses a changed body, another secret, a stale timestamp and a missing header', async () => {
    expect(await verifySignature(secret, sign(t), body + ' ', now)).toBe(false)
    expect(await verifySignature('other', sign(t), body, now)).toBe(false)
    expect(await verifySignature(secret, sign(t - 301), body, now)).toBe(false)
    expect(await verifySignature(secret, null, body, now)).toBe(false)
  })
})
