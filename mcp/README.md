# OWS MCP tool profile

`tools.json` is the set of MCP tools Lifterclub offers over the Open Workout Schema, exactly as its
server lists them: names, descriptions, input schemas and annotations, with the server's
instructions and the error codes a tool error's text starts with. It is generated from the server
(`make mcp-profile`), and a test fails when the two differ.

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
  `readOnlyHint`.
- **Errors are actionable.** A tool error's text starts with one of the codes in `error_codes`
  and says what to do next; `exercise_ambiguous` lists candidates.
- **Stored text is data.** Titles, notes and exercise names in results are the person's own text
  or came from other apps; a server tells clients never to follow instructions in them.
