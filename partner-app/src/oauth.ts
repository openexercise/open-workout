// Signing a person in to an Open Workout server, as any OAuth 2.1 client would: find the
// authorization server from a 401 (RFC 9728, then RFC 8414), register (RFC 7591), send the person
// to consent with PKCE (S256), take the code at a loopback address (RFC 8252) and exchange it.

import { createHash, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import type { AddressInfo } from 'node:net'

export interface Discovery {
  /** The protected resource: what tokens are asked for (RFC 8707). */
  resource: string
  authorizationEndpoint: string
  tokenEndpoint: string
  registrationEndpoint: string
}

export interface Tokens {
  accessToken: string
  refreshToken?: string
  scope: string
  expiresAt: Date
}

/** Finds where to sign in, starting from the API's answer to a request without a token. */
export async function discover(apiBase: string, fetcher: typeof fetch = fetch): Promise<Discovery> {
  const probe = await fetcher(new URL('/v1/workouts', apiBase))
  const challenge = probe.headers.get('WWW-Authenticate') ?? ''
  const metadataURL = /resource_metadata="([^"]+)"/.exec(challenge)?.[1]
  if (probe.status !== 401 || !metadataURL) {
    throw new Error(`expected 401 with resource_metadata from ${apiBase}, got ${probe.status} ${challenge}`)
  }
  const resource = (await json(await fetcher(metadataURL))) as { resource: string; authorization_servers?: string[] }
  const issuer = resource.authorization_servers?.[0]
  if (!issuer) {
    throw new Error('the resource names no authorization server')
  }
  const as = (await json(await fetcher(new URL('/.well-known/oauth-authorization-server', issuer)))) as Record<string, string>
  for (const key of ['authorization_endpoint', 'token_endpoint', 'registration_endpoint']) {
    if (!as[key]) {
      throw new Error(`the authorization server's metadata has no ${key}`)
    }
  }
  return {
    resource: resource.resource,
    authorizationEndpoint: as.authorization_endpoint!,
    tokenEndpoint: as.token_endpoint!,
    registrationEndpoint: as.registration_endpoint!,
  }
}

/** Registers a public client for a loopback redirect. A real app registers once and keeps the ID. */
export async function register(d: Discovery, redirectUri: string, name: string, fetcher: typeof fetch = fetch): Promise<string> {
  const res = await fetcher(d.registrationEndpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      client_name: name,
      redirect_uris: [redirectUri],
      token_endpoint_auth_method: 'none',
      grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'],
    }),
  })
  const body = (await json(res)) as { client_id?: string }
  if (!body.client_id) {
    throw new Error('registration returned no client_id')
  }
  return body.client_id
}

/** A PKCE verifier and its S256 challenge. */
export function pkce(): { verifier: string; challenge: string } {
  const verifier = randomBytes(32).toString('base64url')
  return { verifier, challenge: createHash('sha256').update(verifier).digest('base64url') }
}

export interface SignInOptions {
  apiBase: string
  scope: string
  clientName?: string
  /** Shows the person the consent page: open a browser, or print the URL. */
  openBrowser: (url: string) => void | Promise<void>
  fetcher?: typeof fetch
  /** How long to wait for the person, in milliseconds. */
  timeoutMs?: number
}

/**
 * Signs a person in: discovers, registers, opens the consent page and waits at a loopback address
 * for the code, then exchanges it. The state parameter ties the answer to this request.
 */
export async function signIn(options: SignInOptions): Promise<Tokens & { clientId: string; discovery: Discovery }> {
  const fetcher = options.fetcher ?? fetch
  const d = await discover(options.apiBase, fetcher)
  const { verifier, challenge } = pkce()
  const state = randomBytes(16).toString('base64url')

  let resolve!: (code: string) => void
  let reject!: (err: Error) => void
  const gotCode = new Promise<string>((res, rej) => {
    resolve = res
    reject = rej
  })
  // The answer can arrive, or fail, while the consent page is still being opened; it's awaited
  // below, so mark it handled now.
  gotCode.catch(() => {})
  const server = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://127.0.0.1')
    if (url.pathname !== '/callback') {
      res.writeHead(404).end()
      return
    }
    const error = url.searchParams.get('error')
    if (url.searchParams.get('state') !== state) {
      res.writeHead(400).end('state mismatch')
      reject(new Error('the answer was for another sign-in (state mismatch)'))
    } else if (error) {
      res.writeHead(400).end(`sign-in failed: ${error}`)
      reject(new Error(`sign-in failed: ${error}`))
    } else {
      res.writeHead(200, { 'Content-Type': 'text/plain' }).end('Signed in. You can close this tab.')
      resolve(url.searchParams.get('code') ?? '')
    }
  })
  await new Promise<void>((res) => server.listen(0, '127.0.0.1', res))
  const redirectUri = `http://127.0.0.1:${(server.address() as AddressInfo).port}/callback`
  const timer = setTimeout(() => reject(new Error('timed out waiting for the person to sign in')), options.timeoutMs ?? 5 * 60_000)
  try {
    const clientId = await register(d, redirectUri, options.clientName ?? 'Open Workout example app', fetcher)
    const authorize = new URL(d.authorizationEndpoint)
    authorize.search = new URLSearchParams({
      response_type: 'code',
      client_id: clientId,
      redirect_uri: redirectUri,
      scope: options.scope,
      state,
      code_challenge: challenge,
      code_challenge_method: 'S256',
      resource: d.resource,
    }).toString()
    await options.openBrowser(authorize.toString())
    const code = await gotCode
    const tokens = await tokenRequest(
      d,
      { grant_type: 'authorization_code', code, redirect_uri: redirectUri, client_id: clientId, code_verifier: verifier, resource: d.resource },
      fetcher,
    )
    return { ...tokens, clientId, discovery: d }
  } finally {
    clearTimeout(timer)
    server.close()
  }
}

/**
 * Refreshes. The server sends a new refresh token every time: keep it and drop the old one, since
 * using a refresh token twice ends the whole sign-in.
 */
export async function refresh(d: Discovery, clientId: string, refreshToken: string, fetcher: typeof fetch = fetch): Promise<Tokens> {
  return tokenRequest(d, { grant_type: 'refresh_token', refresh_token: refreshToken, client_id: clientId }, fetcher)
}

async function tokenRequest(d: Discovery, form: Record<string, string>, fetcher: typeof fetch): Promise<Tokens> {
  const res = await fetcher(d.tokenEndpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams(form).toString(),
  })
  const body = (await json(res)) as { access_token?: string; refresh_token?: string; scope?: string; expires_in?: number; error?: string }
  if (!res.ok || !body.access_token) {
    throw new Error(`token request failed: ${body.error ?? res.status}`)
  }
  return {
    accessToken: body.access_token,
    refreshToken: body.refresh_token,
    scope: body.scope ?? '',
    expiresAt: new Date(Date.now() + (body.expires_in ?? 3600) * 1000),
  }
}

async function json(res: Response): Promise<unknown> {
  const text = await res.text()
  try {
    return JSON.parse(text)
  } catch {
    throw new Error(`${res.url}: ${res.status}, not JSON: ${text.slice(0, 200)}`)
  }
}
