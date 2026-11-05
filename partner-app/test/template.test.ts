import { type ChildProcess, execFileSync, spawn } from 'node:child_process'
import { mkdtempSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createOpenWorkoutClient, unwrap } from '@openexercise/open-workout-client'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { Mirror } from '../src/mirror.js'
import { type Delivery, startReceiver } from '../src/receiver.js'

// The app against a real Open Workout server: the template in ../mcp/template, built and run here
// (it needs Go). The template has no OAuth server, so the app uses its fixed token; signing in is
// covered in oauth.test.ts.
let template: ChildProcess
let base = ''
const token = 'partner-app-test'

async function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const s = createServer().listen(0, '127.0.0.1', () => {
      const port = (s.address() as { port: number }).port
      s.close(() => resolve(port))
    })
  })
}

beforeAll(async () => {
  const bin = join(mkdtempSync(join(tmpdir(), 'ows-template-')), 'template')
  execFileSync('go', ['build', '-o', bin, '.'], { cwd: join(import.meta.dirname, '..', '..', 'mcp', 'template'), stdio: 'inherit' })
  const port = await freePort()
  base = `http://127.0.0.1:${port}`
  template = spawn(bin, ['-addr', `127.0.0.1:${port}`, '-allow-http-webhooks'], {
    env: { ...process.env, OWS_TEMPLATE_TOKEN: token },
    stdio: 'inherit',
  })
  for (let i = 0; i < 100; i++) {
    try {
      await fetch(`${base}/v1/workouts`)
      return
    } catch {
      await new Promise((r) => setTimeout(r, 100))
    }
  }
  throw new Error('the template server did not start')
}, 120_000)

afterAll(() => {
  template?.kill()
})

describe('against the template server', () => {
  it('logs, reads back, mirrors and receives signed webhooks', async () => {
    const api = createOpenWorkoutClient({ baseUrl: base, token: () => token })

    const deliveries: Delivery[] = []
    const secrets = new Map<string, string>()
    const receiver = await startReceiver({ secretFor: (id) => secrets.get(id), onDelivery: (d) => deliveries.push(d) })
    const sub = unwrap(
      await api.POST('/v1/webhooks', { body: { url: receiver.url, events: ['workout.created', 'workout.deleted'] } }),
    )
    secrets.set(sub.id, sub.secret ?? '')

    const mirror = new Mirror(new Date(Date.now() - 60_000))
    const externalId = `example-${Date.now()}`
    const body = {
      external_id: externalId,
      title: 'Example app workout',
      exercises: [{ name: 'Barbell Back Squat', sets: [{ reps: 5, load: 100, unit: 'kg' as const }] }],
    }
    const logged = unwrap(await api.POST('/v1/workouts', { body }))
    const id = logged.workout.id ?? ''
    expect(logged.summary).toContain('Barbell Back Squat')

    // Sending the same external ID again is the same workout, not a second one.
    const again = unwrap(await api.POST('/v1/workouts', { body }))
    expect(again.workout.id).toBe(id)

    const read = unwrap(await api.GET('/v1/workouts/{id}', { params: { path: { id } } }))
    expect(read.source.external_id).toBe(externalId)

    await mirror.sync(api)
    expect(mirror.workouts.has(id)).toBe(true)

    unwrap(await api.DELETE('/v1/workouts/{id}', { params: { path: { id } } }))
    await mirror.sync(api)
    expect(mirror.workouts.has(id)).toBe(false)

    // Deliveries arrive in the background.
    for (let i = 0; i < 50 && deliveries.length < 2; i++) {
      await new Promise((r) => setTimeout(r, 100))
    }
    expect(deliveries.map((d) => d.event).sort()).toEqual(['workout.created', 'workout.deleted'])
    expect(deliveries.every((d) => d.workout_id === id)).toBe(true)

    // A delivery with a wrong signature is refused.
    const forged = await fetch(receiver.url, {
      method: 'POST',
      headers: { 'OWS-Signature': `t=${Math.floor(Date.now() / 1000)},v1=00` },
      body: JSON.stringify({ event: 'workout.deleted', workout_id: 'x', subscription_id: sub.id, delivery_id: 'forged' }),
    })
    expect(forged.status).toBe(401)

    await api.DELETE('/v1/webhooks/{id}', { params: { path: { id: sub.id } } })
    await receiver.close()
  }, 30_000)
})
