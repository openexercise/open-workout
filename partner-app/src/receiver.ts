// Receiving webhooks: check each delivery's OWS-Signature against the raw body before believing
// anything in it, and drop a delivery seen before (OWS-Delivery), since a server may send one
// twice.

import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { signatureHeader, verifySignature } from '@openexercise/open-workout-client'

export interface Delivery {
  event: string
  workout_id: string
  occurred_at: string
  subscription_id: string
  delivery_id: string
}

export interface Receiver {
  url: string
  close: () => Promise<void>
}

/**
 * Listens for deliveries on path at host. secretFor returns a subscription's secret, kept from when
 * it was created. A delivery whose signature doesn't verify gets 401 and never reaches onDelivery.
 */
export async function startReceiver(options: {
  host?: string
  port?: number
  path?: string
  secretFor: (subscriptionId: string) => string | undefined
  onDelivery: (d: Delivery) => void
}): Promise<Receiver> {
  const path = options.path ?? '/webhooks'
  const seen = new Set<string>()
  const server: Server = createServer((req, res) => {
    if (req.method !== 'POST' || req.url !== path) {
      res.writeHead(404).end()
      return
    }
    const chunks: Buffer[] = []
    req.on('data', (c: Buffer) => chunks.push(c))
    req.on('end', () => {
      void (async () => {
        const raw = Buffer.concat(chunks).toString('utf8')
        let d: Delivery
        try {
          d = JSON.parse(raw) as Delivery
        } catch {
          res.writeHead(400).end()
          return
        }
        // The subscription ID is only a hint for which secret to try; the signature decides.
        const secret = options.secretFor(d.subscription_id)
        if (!secret || !(await verifySignature(secret, req.headers[signatureHeader.toLowerCase()] as string | undefined, raw))) {
          res.writeHead(401).end()
          return
        }
        res.writeHead(204).end()
        if (!seen.has(d.delivery_id)) {
          seen.add(d.delivery_id)
          options.onDelivery(d)
        }
      })()
    })
  })
  await new Promise<void>((resolve) => server.listen(options.port ?? 0, options.host ?? '127.0.0.1', resolve))
  const { address, port } = server.address() as AddressInfo
  return {
    url: `http://${address}:${port}${path}`,
    close: () => new Promise((resolve) => server.close(() => resolve())),
  }
}
