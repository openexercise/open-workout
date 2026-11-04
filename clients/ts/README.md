# @openexercise/lifterclub-client

A thin TypeScript client for the [Lifterclub API](https://lifterclub.com/developers): the
types generated from the API's OpenAPI document (`src/v1.d.ts`), an
[openapi-fetch](https://openapi-ts.dev/openapi-fetch/) client that sends your token and an
`Idempotency-Key` on every POST, and a few helpers. Workouts are
[Open Workout Schema](https://openworkout.dev) documents.

```bash
npm install @openexercise/lifterclub-client
```

Published from [openexercise/open-workout](https://github.com/openexercise/open-workout/tree/main/clients/ts), where a tag `clients-ts-v<version>` releases it with npm provenance.

## Use

```ts
import { changes, createLifterclubClient, unwrap, verifySignature } from '@openexercise/lifterclub-client'

const api = createLifterclubClient({ token: () => tokens.access })

// Every route in the API, typed from the document.
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

// A webhook delivery.
const ok = await verifySignature(subscription.secret, request.headers.get('Lifterclub-Signature'), rawBody)
```

`api.GET`, `POST`, `PATCH`, `PUT` and `DELETE` return `{ data, error, response }`; `unwrap` returns
`data` or throws an `ApiError` whose `code` is the API's error code (`rate_limited`, `not_found`,
`invalid_token`…), with `status`, `detail`, the full `problem`, and `retryAfter` in seconds when
the status is 429. Limits are per account and per app; the problem's `detail` says which.

A POST retried with the same `Idempotency-Key` within a day returns the first result instead of
writing twice. The client makes a key per call; pass your own in `headers` to retry across
restarts.

## Develop

- `npm run gen` regenerates `src/v1.d.ts` from the OpenAPI document (the Lifterclub repository's
  `docs/openapi/v1.yaml`, or `api/v1.yaml` next to this package; `SPEC=path` names another).
- `npm run check` fails when the committed types are stale.
- `npm test`, `npm run typecheck`, `npm run build`.
