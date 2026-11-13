// The example app, start to finish: sign in (or take a token), log a workout with an external ID,
// read it back, mirror the log through the change feed, and with --webhooks receive signed
// deliveries.
//
//   npm start -- https://api.example.com            sign in with OAuth in a browser
//   OWS_TOKEN=… npm start -- http://localhost:8080  use a token you already have
//   npm start -- http://localhost:8080 --webhooks   also subscribe and receive webhooks (http
//                                                   receivers need a server that allows them)
//   npm start -- https://api.example.com --print-token   sign in, print the access token, stop

import { createOpenWorkoutClient, unwrap } from '@openexercise/open-workout-client'
import { Mirror } from './mirror.js'
import { signIn } from './oauth.js'
import { startReceiver } from './receiver.js'

const args = process.argv.slice(2)
const apiBase = args.find((a) => !a.startsWith('--'))
if (!apiBase) {
  console.error('usage: npm start -- <api-base-url> [--webhooks]')
  process.exit(2)
}

let token = process.env.OWS_TOKEN
if (!token) {
  const tokens = await signIn({
    apiBase,
    scope: 'workouts:read workouts:write',
    openBrowser: (url) => console.log(`Open this address to sign in:\n${url}\n`),
  })
  token = tokens.accessToken
  if (args.includes('--print-token')) {
    console.log(token)
    process.exit(0)
  }
  console.log(`Signed in, with ${tokens.scope}.`)
}
const api = createOpenWorkoutClient({ baseUrl: apiBase, token: () => token })

const secrets = new Map<string, string>()
let receiver: Awaited<ReturnType<typeof startReceiver>> | undefined
if (args.includes('--webhooks')) {
  receiver = await startReceiver({
    secretFor: (id) => secrets.get(id),
    onDelivery: (d) => console.log(`webhook: ${d.event} ${d.workout_id}`),
  })
  const sub = unwrap(
    await api.POST('/v1/webhooks', { body: { url: receiver.url, events: ['workout.created', 'workout.updated', 'workout.deleted'] } }),
  )
  secrets.set(sub.id, sub.secret ?? '')
  console.log(`Subscribed ${receiver.url}`)
}

// Log. The external ID is this app's own ID for the workout: sending it again never logs twice.
const logged = unwrap(
  await api.POST('/v1/workouts', {
    body: {
      external_id: `example-${Date.now()}`,
      title: 'Example app workout',
      exercises: [{ name: 'Barbell Back Squat', sets: [{ reps: 5, load: 100, unit: 'kg' }, { reps: 5, load: 100, unit: 'kg' }] }],
    },
  }),
)
console.log(`Logged ${logged.workout.id}.`)
for (const c of logged.needs_confirmation) {
  console.log(`  confirm "${c.text}" with the person: ${c.candidates.map((x) => x.name).join(', ') || 'no candidates'}`)
}

const id = logged.workout.id ?? ''
const read = unwrap(await api.GET('/v1/workouts/{id}', { params: { path: { id } } }))
console.log(`Read back ${read.id}: ${read.exercises.length} exercise(s).`)

const mirror = new Mirror(new Date(Date.now() - 24 * 3600 * 1000))
await mirror.sync(api)
console.log(`Mirror holds ${mirror.workouts.size} workout(s) changed in the last day.`)

if (receiver) {
  await new Promise((resolve) => setTimeout(resolve, 2000))
  for (const subscription of secrets.keys()) {
    await api.DELETE('/v1/webhooks/{id}', { params: { path: { id: subscription } } })
  }
  await receiver.close()
}
