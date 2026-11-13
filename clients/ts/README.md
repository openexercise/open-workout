# @openexercise/open-workout-client

A thin TypeScript client for any [Open Workout](https://openworkout.dev) or
[Open Food Log](https://openfoodlog.org) server. It's an
[openapi-fetch](https://openapi-ts.dev/openapi-fetch/) client typed from the Open Workout API core
(`api/core.yaml`) or the Open Food Log API core (`api/ofl-core.yaml`), which sends your token and
an `Idempotency-Key` on every POST, plus a few helpers. Workouts are Open Workout Schema
documents, exercises are [Open Exercise](https://openexercise.org) IDs, and meals and foods are
Open Food Log documents.

```bash
npm install @openexercise/open-workout-client
```

Three entry points:
- `@openexercise/open-workout-client`: the Open Workout core, for any server.
  `createOpenWorkoutClient` takes the server's address.
- `@openexercise/open-workout-client/openfoodlog`: the Open Food Log core, for any server.
  `createOpenFoodLogClient` takes the server's address; `mealChanges` follows the meals change
  feed.
- `@openexercise/open-workout-client/lifterclub`: [Lifterclub](https://lifterclub.com/developers),
  the reference implementation. `createLifterclubClient` is typed from Lifterclub's full API (both
  cores plus sentence parsing, records and progress, the profile, routines, programs, imports and
  partners), and defaults to `https://api.lifterclub.com`.

Published from [openexercise/open-workout](https://github.com/openexercise/open-workout/tree/main/clients/ts), where a tag `clients-ts-v<version>` releases it with npm provenance.

## Use

```ts
import { changes, createOpenWorkoutClient, signatureHeader, unwrap, verifySignature } from '@openexercise/open-workout-client'

const api = createOpenWorkoutClient({ baseUrl: 'https://api.lifterclub.com', token: () => tokens.access })

// Every route in the core, typed from the document.
const logged = unwrap(
  await api.POST('/v1/workouts', {
    body: { external_id: 'my-42', exercises: [{ name: 'bench press', sets: [{ reps: 5, load: 185, unit: 'lb' }] }] },
  }),
)
console.log(logged.summary)

// Mirror a log: every change since the last time, oldest first, deleted ones as tombstones.
for await (const change of changes(api, { updatedSince: lastSeen, includeDeleted: true })) {
  if (change.kind === 'workout') store(change.workout)
  else remove(change.tombstone.id)
}

// A webhook delivery, signed in OWS-Signature.
const ok = await verifySignature(subscription.secret, request.headers.get(signatureHeader), rawBody)
```

A food log, the same way:

```ts
import { createOpenFoodLogClient, mealChanges, signatureHeader, unwrap } from '@openexercise/open-workout-client/openfoodlog'

const food = createOpenFoodLogClient({ baseUrl: 'https://api.lifterclub.com', token: () => tokens.access })
const written = unwrap(
  await food.POST('/v1/meals', { body: { external_id: 'my-7', items: [{ text: 'banana', food_id: 'ofc:bananas-raw' }] } }),
)
for await (const change of mealChanges(food, { updatedSince: lastSeen, includeDeleted: true })) {
  if (change.kind === 'meal') store(change.meal)
  else remove(change.tombstone.id)
}
// Meal deliveries are signed in OFL-Signature (signatureHeader here), the same scheme.
```

For Lifterclub's routes beyond the cores:

```ts
import { createLifterclubClient, unwrap } from '@openexercise/open-workout-client/lifterclub'

const lc = createLifterclubClient({ token: () => tokens.access })
const draft = unwrap(await lc.POST('/v1/parse', { body: { text: 'bench 3x8 at 185' } }))
```

`api.GET`, `POST`, `PATCH`, `PUT` and `DELETE` return `{ data, error, response }`. `unwrap`
returns `data`, or throws an `ApiError`. Its `code` is the error code (`rate_limited`,
`not_found`, `invalid_token`…), with `status`, `detail`, the full `problem`, and `retryAfter` in
seconds when the status is 429.

A POST retried with the same `Idempotency-Key` returns the first result instead of writing
twice. The client makes a key per call; pass your own in `headers` to retry across restarts.

Lifterclub also signs webhooks as `Lifterclub-Signature` (`lifterclubSignatureHeader`) until
OWS v1, for receivers written before.

## Moving from `@openexercise/lifterclub-client`

That package was this one under its earlier name, up to 0.3.0. Install this one instead, and
import from `@openexercise/open-workout-client/lifterclub` to keep `createLifterclubClient` and
every type as they were. Read `OWS-Signature` (`signatureHeader`) on new webhook receivers.

## Develop

- `npm run gen` regenerates `src/core.d.ts`, `src/ofl-core.d.ts` and `src/lifterclub-api.d.ts`
  from the OpenAPI documents: the Lifterclub repository's `docs/openapi/ows-core.yaml`,
  `ofl-core.yaml` and `v1.yaml`, or `api/core.yaml`, `api/ofl-core.yaml` and `api/v1.yaml` next to
  this package. `CORE_SPEC=path`, `OFL_CORE_SPEC=path` and `SPEC=path` name other copies.
- `npm run check` fails when the committed types are stale.
- `npm test`, `npm run typecheck`, `npm run build`.
