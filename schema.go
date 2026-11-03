// Package openworkout carries the published schemas, so tools built from this module (the
// conformance suite, a server's own tests) can validate documents without a checkout.
package openworkout

import "embed"

// Schemas holds schema/v0, the documents served at https://openworkout.dev/schema/v0/.
//
//go:embed schema/v0/*.json
var Schemas embed.FS
