# Open Workout

An open format for a workout log, and the set of MCP tools an assistant uses to read and write one.

**Status: v0 development draft.** The schema may change until v1, and every change is listed in its changelog. v1 freezes when a second application writes it in production; after that, changes are additive.

| | |
| --- | --- |
| `schema/v0/workout.json` | The Open Workout Schema (OWS): one workout, its exercise entries and sets, where the record came from, and every inferred value listed by JSON pointer |
| `schema/v0/program.json` | A training program as a library publishes it |
| `schema/CHANGELOG.md` | What changed in v0 |
| `mcp/tools.json` | The OWS MCP tool profile: the tools, their input schemas and annotations, generated from the reference server |
| `mcp/README.md` | What a server offering those tools keeps to |
| `examples` | A workout and a program in the format |
| `cmd/ows` | `ows validate`, which checks documents against the schemas, and `ows site`, which builds the website |

Until v1 these files are published here from [Lifterclub](https://lifterclub.com), the reference implementation, and the schema is validated against real output there. Issues and proposals belong here.

## Use it

```bash
go run ./cmd/ows validate            # the examples
go run ./cmd/ows validate my-log.json # your own documents, files or directories
go test ./...
go run ./cmd/ows site dist           # the website into dist
```

A document names its schema in `$schema`; without one it is checked as a workout.

The site at [openworkout.dev](https://openworkout.dev) is built from this repository by `ows site` and deployed by the Pages workflow on every push to `main`, so the schemas resolve at their `$id` URLs.

## Coming

- `ows conformance`, which runs a fixed script against any server offering the reference REST API.
- Client libraries.

## Relationship to other standards

An OWS entry refers to its exercise by an [Open Exercise](https://github.com/openexercise/open-exercise) ID (`oec:barbell-bench-press`). Open Exercise says what an exercise is; Open Workout records what someone did.

## Licence

[Apache-2.0](LICENSE).
