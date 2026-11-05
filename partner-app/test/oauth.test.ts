import { createHash } from 'node:crypto'
import { createServer, type IncomingMessage, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { discover, refresh, signIn } from '../src/oauth.js'

// A stub API and authorization server on one origin, enough to sign in against: a 401 that names
// the resource metadata, the metadata documents, open registration, an authorize endpoint that
// approves at once, and a token endpoint that checks PKCE and rotates refresh tokens.
let server: Server
let base = ''
const codes = new Map<string, { challenge: string; clientId: string; redirect: string }>()
const refreshTokens = new Set<string>()

function body(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let s = ''
    req.on('data', (c: Buffer) => (s += c.toString()))
    req.on('end', () => resolve(s))
  })
}

beforeAll(async () => {
  server = createServer(async (req, res) => {
    const url = new URL(req.url ?? '/', base)
    const send = (status: number, v: unknown) => res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(v))
    switch (url.pathname) {
      case '/v1/workouts':
        res.writeHead(401, { 'WWW-Authenticate': `Bearer resource_metadata="${base}/.well-known/oauth-protected-resource"` }).end()
        return
      case '/.well-known/oauth-protected-resource':
        return send(200, { resource: base, authorization_servers: [base] })
      case '/.well-known/oauth-authorization-server':
        return send(200, {
          issuer: base,
          authorization_endpoint: `${base}/oauth/authorize`,
          token_endpoint: `${base}/oauth/token`,
          registration_endpoint: `${base}/oauth/register`,
        })
      case '/oauth/register': {
        const meta = JSON.parse(await body(req)) as { redirect_uris: string[] }
        return send(201, { client_id: `client-${meta.redirect_uris[0]}` })
      }
      case '/oauth/authorize': {
        const q = url.searchParams
        if (q.get('code_challenge_method') !== 'S256' || q.get('resource') !== base) {
          return send(400, { error: 'invalid_request' })
        }
        const code = `code-${codes.size}`
        codes.set(code, { challenge: q.get('code_challenge') ?? '', clientId: q.get('client_id') ?? '', redirect: q.get('redirect_uri') ?? '' })
        const back = new URL(q.get('redirect_uri') ?? '')
        back.searchParams.set('code', code)
        back.searchParams.set('state', q.get('state') ?? '')
        res.writeHead(302, { Location: back.toString() }).end()
        return
      }
      case '/oauth/token': {
        const form = new URLSearchParams(await body(req))
        if (form.get('grant_type') === 'refresh_token') {
          const old = form.get('refresh_token') ?? ''
          if (!refreshTokens.delete(old)) {
            return send(400, { error: 'invalid_grant' })
          }
          refreshTokens.add(`${old}+`)
          return send(200, { access_token: 'access-2', refresh_token: `${old}+`, scope: 'workouts:read', expires_in: 3600 })
        }
        const grant = codes.get(form.get('code') ?? '')
        codes.delete(form.get('code') ?? '')
        const verified = createHash('sha256').update(form.get('code_verifier') ?? '').digest('base64url')
        if (!grant || grant.challenge !== verified || grant.clientId !== form.get('client_id') || grant.redirect !== form.get('redirect_uri')) {
          return send(400, { error: 'invalid_grant' })
        }
        refreshTokens.add('refresh-1')
        return send(200, { access_token: 'access-1', refresh_token: 'refresh-1', scope: 'workouts:read workouts:write', expires_in: 3600 })
      }
    }
    res.writeHead(404).end()
  })
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
})

afterAll(() => new Promise<void>((resolve) => server.close(() => resolve())))

describe('OAuth with PKCE', () => {
  it('discovers the authorization server from a 401', async () => {
    const d = await discover(base)
    expect(d.resource).toBe(base)
    expect(d.tokenEndpoint).toBe(`${base}/oauth/token`)
  })

  it('signs in through the consent page and refreshes', async () => {
    // The "browser" follows the consent page's redirect back to the app's loopback address.
    const tokens = await signIn({
      apiBase: base,
      scope: 'workouts:read workouts:write',
      openBrowser: async (url) => {
        await fetch(url)
      },
      timeoutMs: 10_000,
    })
    expect(tokens.accessToken).toBe('access-1')
    expect(tokens.scope).toBe('workouts:read workouts:write')

    const next = await refresh(tokens.discovery, tokens.clientId, tokens.refreshToken ?? '')
    expect(next.accessToken).toBe('access-2')
    // The old refresh token is spent: keep the new one.
    await expect(refresh(tokens.discovery, tokens.clientId, tokens.refreshToken ?? '')).rejects.toThrow(/invalid_grant/)
  })

  it('refuses an answer meant for another sign-in', async () => {
    await expect(
      signIn({
        apiBase: base,
        scope: 'workouts:read',
        openBrowser: async (url) => {
          const u = new URL(url)
          const back = new URL(u.searchParams.get('redirect_uri') ?? '')
          back.searchParams.set('code', 'stolen')
          back.searchParams.set('state', 'someone-else')
          await fetch(back)
        },
        timeoutMs: 10_000,
      }),
    ).rejects.toThrow(/state mismatch/)
  })
})
