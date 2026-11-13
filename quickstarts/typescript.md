# TypeScript quickstart

The example app in [`partner-app`](../partner-app) is a whole client on
`@openexercise/open-workout-client`: OAuth 2.1 with PKCE found from a 401, logging with an
external ID, reading back, mirroring the log through the change feed, and a webhook receiver that
checks signatures. Run it, then take what you need.

## Run it

Node 22 or later:

```bash
cd partner-app
npm install
npm start -- https://lifterclub-api.runmighty.dev
```

It prints a sign-in address: open it, sign in to the sandbox with Google and allow the app. Then it
logs a workout, reads it back and mirrors the last day of the log.

Against the template server on your machine (see [Where to run them](README.md#where-to-run-them)),
with its fixed token and webhooks too:

```bash
OWS_TOKEN=dev-token npm start -- http://localhost:8080 --webhooks
```

## The parts

**Sign in** ([`src/oauth.ts`](../partner-app/src/oauth.ts)). Ask the API without a token: the
`401` names the protected resource metadata, which names the authorization server, whose metadata
names the endpoints. Register (once, and keep the `client_id`), send the person to the consent page
with a PKCE challenge, take the code at a loopback address, and exchange it with the verifier.
Every refresh returns a new refresh token: keep it and drop the old one, since using one twice ends
the sign-in.

```ts
import { signIn } from './oauth.js'

const tokens = await signIn({ apiBase, scope: 'workouts:read workouts:write', openBrowser: (url) => open(url) })
```

**Log and read back.** The client adds an `Idempotency-Key` to every POST, so a retry never logs
twice; `external_id` is your own ID for the workout, so sending it again later is the same workout,
and one the person deleted stays deleted.

```ts
import { createOpenWorkoutClient, unwrap } from '@openexercise/open-workout-client'

const api = createOpenWorkoutClient({ baseUrl: apiBase, token: () => tokens.accessToken })
const logged = unwrap(await api.POST('/v1/workouts', {
  body: { external_id: 'my-42', exercises: [{ name: 'back squat', sets: [{ reps: 5, load: 100, unit: 'kg' }] }] },
}))
console.log(`Logged ${logged.workout.id}`)
for (const c of logged.needs_confirmation) {
  // Ask the person which exercise they meant (c.candidates); never guess. Then PATCH /v1/entries/{id}.
}
```

**Mirror the log** ([`src/mirror.ts`](../partner-app/src/mirror.ts)). Read the change feed from
the last `updated_at` you saw: changed workouts oldest first, deleted ones as tombstones.

```ts
for await (const change of changes(api, { updatedSince: lastSeen, includeDeleted: true })) {
  if (change.kind === 'workout') save(change.workout)
  else remove(change.tombstone.id)
}
```

**Receive webhooks** ([`src/receiver.ts`](../partner-app/src/receiver.ts)). Subscribe with
`POST /v1/webhooks` and keep the `secret` from the answer: it's shown once. Check every delivery's
`OWS-Signature` against the raw body before trusting it, and ignore a `OWS-Delivery` you've seen.

```ts
const ok = await verifySignature(secret, request.headers['ows-signature'], rawBody)
```

A delivery only says which workout changed; read it (or sync the change feed) to see how.

## Keep to

- Log only what the person said, and mark anything you inferred `estimated`.
- Confirm anything in `needs_confirmation` with the person before you treat it as settled.
- Wait out `Retry-After` on `rate_limited`.

`partner-app`'s tests run all of this against the template server in CI (`npm test`, with Go
installed).
