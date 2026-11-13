import { describe, expect, it } from 'vitest'

import { ApiError, createOpenFoodLogClient, mealChanges, signatureHeader, unwrap } from './openfoodlog.js'

type Handler = (request: Request) => Response | Promise<Response>

function fakeApi(handler: Handler) {
  const requests: Request[] = []
  const api = createOpenFoodLogClient({
    baseUrl: 'https://food.test/',
    token: 'lca_test',
    idempotencyKey: () => 'key-1',
    fetch: async (request) => {
      requests.push(request)
      return handler(request)
    },
  })
  return { api, requests }
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const meal = (id: string) => ({
  $schema: 'https://openfoodlog.org/schema/v0/meal.json' as const,
  id,
  date: '2026-11-12',
  updated_at: '2026-11-12T10:00:00Z',
  source: { kind: 'api', external_id: null },
  items: [],
  estimates: [],
})

describe('createOpenFoodLogClient', () => {
  it('sends the bearer token and an Idempotency-Key when logging a meal', async () => {
    const { api, requests } = fakeApi(() => json({ meal: meal('m1'), needs_confirmation: [] }, 201))
    const written = unwrap(
      await api.POST('/v1/meals', { body: { external_id: 'app-1', items: [{ text: 'a bar', nutrients: { energy_kcal: 200 } }] } }),
    )
    expect(written.meal.id).toBe('m1')
    expect(requests[0]?.url).toBe('https://food.test/v1/meals')
    expect(requests[0]?.headers.get('Authorization')).toBe('Bearer lca_test')
    expect(requests[0]?.headers.get('Idempotency-Key')).toBe('key-1')
  })

  it('throws an ApiError with the problem code', async () => {
    const { api } = fakeApi(
      () =>
        new Response(JSON.stringify({ type: 'about:blank', title: 'Not found', status: 404, code: 'not_found' }), {
          status: 404,
          headers: { 'Content-Type': 'application/problem+json' },
        }),
    )
    const result = await api.GET('/v1/meals/{id}', { params: { path: { id: 'gone' } } })
    expect(() => unwrap(result)).toThrow(ApiError)
  })

  it('names the Open Food Log signature header', () => {
    expect(signatureHeader).toBe('OFL-Signature')
  })
})

describe('mealChanges', () => {
  it('follows next_cursor and yields meals, then tombstones', async () => {
    const { api, requests } = fakeApi((request) => {
      const url = new URL(request.url)
      if (!url.searchParams.get('cursor')) {
        return json({ meals: [meal('m1')], truncated: true, next_cursor: 'c2' })
      }
      return json({ meals: [], deleted: [{ id: 'm2', deleted_at: '2026-11-12T11:00:00Z', updated_at: '2026-11-12T11:00:00Z' }] })
    })
    const seen: string[] = []
    for await (const change of mealChanges(api, { updatedSince: new Date('2026-11-12T00:00:00Z'), includeDeleted: true })) {
      seen.push(change.kind === 'meal' ? `meal ${change.meal.id}` : `deleted ${change.tombstone.id}`)
    }
    expect(seen).toEqual(['meal m1', 'deleted m2'])
    const first = new URL(requests[0]!.url)
    expect(first.searchParams.get('updated_since')).toBe('2026-11-12T00:00:00.000Z')
    expect(first.searchParams.get('include_deleted')).toBe('true')
    expect(new URL(requests[1]!.url).searchParams.get('cursor')).toBe('c2')
  })
})
