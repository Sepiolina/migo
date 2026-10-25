// Package workflows holds the built-in workflow files. Each <name>.yaml is
// served at /api/<name>/upload and /api/<name>/download. A file with the same
// name in <config dir>/workflows/ replaces it; new names add endpoints.
package workflows

import "embed"

//go:embed *.yaml
var FS embed.FS
