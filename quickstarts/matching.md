# Matching quickstart

Matching turns the names people use ("db bench", "rdl") into [Open Exercise](https://openexercise.org)
IDs, for an app with nobody signed in: say, to map the exercise list in your own app once. It's
Lifterclub's AI service, not part of the core, so it runs against the sandbox or production, not the
template server.

Your app uses a token of its own (the client credentials grant), which reaches matching and
nothing of anyone's.

## 1. Register a confidential client

Once, and keep the secret:

```bash
curl -s https://lifterclub-api.runmighty.dev/oauth/register \
  -H 'Content-Type: application/json' \
  -d '{"client_name": "My App", "grant_types": ["client_credentials"], "token_endpoint_auth_method": "client_secret_basic"}'
```

The answer has `client_id` (`lcc_…`) and `client_secret` (`lcs_…`).

## 2. Take a token

```bash
curl -s https://lifterclub-api.runmighty.dev/oauth/token \
  -u "$CLIENT_ID:$CLIENT_SECRET" \
  -d grant_type=client_credentials
```

The `access_token` lasts an hour and carries only `catalogue:match`. There's no refresh token: ask
again.

## 3. Match

```bash
curl -s https://lifterclub-api.runmighty.dev/v1/match \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"names": ["db bench", "rdl", "flurbo lift"]}'
```

Each name comes back in order with a `status`: `catalogue` is sure, `close` is likely (let your
user confirm it), `unmatched` has no exercise. `candidates` lists the alternatives, best first. Up
to 50 names a call; one call counts once against your app's budget. Nothing is stored.

From TypeScript or anything else, it's one request:

```ts
const res = await fetch('https://lifterclub-api.runmighty.dev/v1/match', {
  method: 'POST',
  headers: { Authorization: `Bearer ${appToken}`, 'Content-Type': 'application/json' },
  body: JSON.stringify({ names: ['db bench', 'rdl'] }),
})
const { matches } = await res.json()
```

Assistants have the same matching as the MCP tool `match_exercises`.
