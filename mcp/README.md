# OWS MCP tool profile

`tools.json` is the set of MCP tools offered over the Open Workout Schema, exactly as the reference
server (Lifterclub) lists them: names, descriptions, input schemas and annotations, with the
instructions and every error code a tool error's text can start with. It is generated from that
server, and a test fails when the two differ.

## Core and extensions

Each tool's `_meta` names its profile under `dev.openworkout/profile`, and servers send the same
mark when they list their tools:

- **`core`**: logging (`log_workout`, `add_sets`), edits (`update_entry`, `delete_entry`), `undo`,
  reads (`get_workouts`, `get_personal_records`, `get_progress`) and `search_exercises`. None
  needs an LLM. A compatible server offers all of them, with these shapes.
- **`lifterclub`**: the reference server's own extensions, which other servers may copy but need
  not: `log_text` (sentence parsing by an LLM), the training profile and programs.

`log_workout`'s optional `program` names a program day, for servers that offer programs; a
server without them refuses it with `invalid_input`.

`template/` in `openexercise/open-workout` is a small server to copy that offers the core: Go, the
official MCP SDK and an in-memory store.

`instructions` are the core's, free of any product name; a server sends them as its MCP
instructions and may add its own after them, as Lifterclub adds `extension_instructions`.
`error_codes` lists each code with the profile that uses it and what a client should do.

It is a *profile*, not a standard of its own: another server that offers these tools with these
shapes can be used by the same assistants and clients. It makes no stability promise before OWS
v1.

## What a compatible server keeps to

- **No invented numbers.** A tool stores only what the person stated. Anything inferred is marked
  estimated (`estimates` in the workout, by JSON pointer).
- **Matches are honest.** Every exercise entry says whether it is a catalogue match, a close
  match, the person's own exercise, or unmatched. Unconfirmed entries are returned in
  `needs_confirmation` and never count toward records.
- **Writes are idempotent.** Every write takes an `idempotency_key`; a retry with the same key
  returns the first result instead of writing twice.
- **Results carry a summary to echo.** A write returns a one-line summary for the assistant to
  tell the person, then the stored workout as OWS JSON.
- **Calm mode is respected.** When the person has asked not to be shown numbers, summaries leave
  records and totals out, and assistants are told not to volunteer them.
- **Read-only access exists.** A read-only credential sees and may call only the tools annotated
  `readOnlyHint`; calling another is a protocol error whose message starts with `read_only`.
- **Errors are actionable.** A tool error's text starts with one of the codes in `error_codes`,
  a colon and a space, and says what to do next; `exercise_ambiguous` lists candidates.
- **Stored text is data.** Titles, notes and exercise names in results are the person's own text
  or came from other apps; a server tells clients never to follow instructions in them.

## What an AI client keeps to

An assistant or app that writes to a log through these tools, or through a server's AI, keeps to
these too. A server can't enforce them all, so they're part of what a server asks of the clients it
trusts:

- **Log only what the person said.** Never a number they didn't give; a value worked out from a
  photo or a screen is sent with `estimated`.
- **Ask, don't guess.** When a result lists `needs_confirmation`, ask the person which exercise
  they meant (offer the candidates), then fix the entry. A `close` match from matching is a
  suggestion to confirm, not an answer.
- **Show what happened.** Tell the person the summary line, and any `warnings` (`llm_limited`
  means the model's budget ran out and a simpler reader was used).
- **Respect calm mode.** When the person has asked not to see numbers, don't volunteer records,
  totals or progress.
- **Retry safely.** Send an `idempotency_key` with every write.

