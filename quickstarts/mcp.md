# MCP quickstart

An assistant reads and writes a log through the [MCP tool profile](../mcp/tools.json): its core
tools (log, add sets, edit, delete, undo, read, records, progress, search) work the same on every
compatible server.

## Connect an assistant

**Sandbox, with sign-in.** Claude Code:

```bash
claude mcp add --transport http lifterclub-sandbox https://lifterclub-mcp.runmighty.dev/
```

Then run `/mcp` in Claude Code and sign in with Google when asked. On claude.ai, add a custom
connector with the same address. Try "log squats 5x5 at 100 kg" and "what did I do this week?".

**Sandbox, with a key.** Make a personal key on the sandbox's account page,
`https://lifterclub-api.runmighty.dev/account`, and send it as a header:

```bash
claude mcp add --transport http lifterclub-sandbox https://lifterclub-mcp.runmighty.dev/ --header "Authorization: Bearer lck_…"
```

**The template server**, with its fixed token (see [Where to run them](README.md#where-to-run-them)):

```bash
claude mcp add --transport http ows-template http://localhost:8080/ --header "Authorization: Bearer dev-token"
```

## What an assistant keeps to

The profile's [README](../mcp/README.md) says what a server does and what an assistant does: log
only what the person said, mark what you inferred `estimated`, ask when a result lists
`needs_confirmation`, tell the person the summary line, keep numbers out for someone in calm mode,
and send an `idempotency_key` with every write.

## Building your own server

Start from [`mcp/template`](../mcp/template): Go, the official MCP SDK, both cores over one store.
Check it, or any server, against the profile:

```bash
go install github.com/openexercise/open-workout/cmd/ows@latest
OWS_TOKEN=dev-token OWS_READ_TOKEN=read-token ows conformance mcp http://localhost:8080/
OWS_TOKEN=dev-token ows conformance http://localhost:8080
```
