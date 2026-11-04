# Governance

Open Workout covers the Open Workout Schema (OWS), the OWS MCP tool profile, the reference REST API document, the conformance checks and the clients. It's a small project, run in the open.

## Maintainers

| Name | GitHub | Role |
| --- | --- | --- |
| Bryan Morrow | [myteedev](https://github.com/myteedev) | Lead maintainer |

Write to the maintainers at maintainers@openexercise.org, or open an issue. Someone who has made sustained, careful contributions can be invited to become a maintainer by the existing ones.

## How decisions are made

- **Changes** start as an issue or proposal here ([CONTRIBUTING.md](CONTRIBUTING.md)), and a maintainer decides after at least a week for comment. Each lands with a changelog entry in `schema/CHANGELOG.md`.
- **Versions:** v0 is a development draft, so breaking changes can still land. v1 freezes when a second application writes OWS in production; after it, changes are additive, and a breaking one needs a new major version through a written proposal (an RFC process set up at that point).

Maintainers aim for agreement in the issue. When there isn't any, the lead maintainer decides and says why in the issue.

## Where the source of truth is

Until v1, the schema, the tool profile, the API document and the clients are published here from [Lifterclub](https://github.com/mightydotrun/lifterclub), the reference implementation, where they are tested against real output. An agreed change is made there and published here. At v1 the source of truth moves to this repository.

## Conduct and security

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md). Report security problems privately, as [SECURITY.md](SECURITY.md) describes.
