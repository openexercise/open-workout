# Example partner app

A small app connecting to an Open Workout server, on
[`@openexercise/open-workout-client`](../clients/ts): what a partner app does, in a few files to
read and copy.

| | |
| --- | --- |
| [`src/oauth.ts`](src/oauth.ts) | Signing a person in: discovery from the API's `401` (RFC 9728, RFC 8414), registration (RFC 7591), the consent page with PKCE (S256) and `state`, the code at a loopback address (RFC 8252), refresh |
| [`src/mirror.ts`](src/mirror.ts) | Keeping a copy of the log through the change feed, deleted workouts included |
| [`src/receiver.ts`](src/receiver.ts) | Receiving webhooks: `OWS-Signature` checked against the raw body, repeats dropped by `OWS-Delivery` |
| [`src/main.ts`](src/main.ts) | All of it in order: sign in, log with an `external_id`, read back, mirror, and with `--webhooks` subscribe and receive |

```bash
npm install
npm start -- https://lifterclub-api.runmighty.dev           # sign in to the sandbox in a browser
OWS_TOKEN=dev-token npm start -- http://localhost:8080 --webhooks   # the template server
npm start -- https://lifterclub-api.runmighty.dev --print-token     # just get a token
```

The [TypeScript quickstart](../quickstarts/typescript.md) walks through it.

## Tests

`npm test` runs both suites: signing in against a stub authorization server, and everything else
against the [template server](../mcp/template), which the test builds with Go and runs. CI runs
them on every change.

It lives here rather than under `examples/`, where every JSON file is checked as an Open Workout
document.
