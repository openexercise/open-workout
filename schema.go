// Package openworkout carries the published schemas and the MCP tool profile, so tools built from this module (the
// conformance suite, a server's own tests) can validate documents without a checkout.
package openworkout

import "embed"

// Schemas holds schema/v0, the documents served at https://openworkout.dev/schema/v0/.
//
//go:embed schema/v0/*.json
var Schemas embed.FS

// Profile is mcp/tools.json, the MCP tool profile, so a checker can compare a server's tools with
// it without a checkout.
//
//go:embed mcp/tools.json
var Profile []byte
