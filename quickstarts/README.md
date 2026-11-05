# Quickstarts

Four ways in, each runnable in a few minutes:

| | |
| --- | --- |
| [TypeScript](typescript.md) | An app signs a person in with OAuth, logs a workout, reads it back, mirrors the log and receives signed webhooks |
| [Go](go.md) | The same from Go, on the standard library |
| [An assistant over MCP](mcp.md) | Connect Claude (or any MCP client) to a log, or check your own MCP server |
| [Matching exercise names](matching.md) | An app maps names to Open Exercise IDs with a token of its own, no person involved |

## Where to run them

- **Locally, against the template server.** `mcp/template` is a small Open Workout server with the
  API core at `/v1` and the MCP core at `/`, keeping its log in memory. It needs Go:

  ```bash
  cd mcp/template
  OWS_TEMPLATE_TOKEN=dev-token OWS_TEMPLATE_READ_TOKEN=read-token go run . -allow-http-webhooks
  ```

  It serves `http://localhost:8080` with fixed tokens instead of sign-in, and forgets everything
  when it stops.

- **Against the sandbox.** Lifterclub, the reference implementation, runs a sandbox: the web app
  at `https://lifterclub.runmighty.dev`, the API and sign-in at
  `https://lifterclub-api.runmighty.dev`, MCP at `https://lifterclub-mcp.runmighty.dev`. Sign in
  with your own Google account; nobody needs to set you up. Use test data only: the sandbox runs
  unreleased code and **its data may be reset at any time**. It has the full Open Exercise
  catalogue, which the template's dozen sample exercises don't.

Production is `https://api.lifterclub.com` and `https://mcp.lifterclub.com`; move there once your
app works. [Lifterclub's developer page](https://lifterclub.com/developers) has its limits and how
to become a verified partner.
