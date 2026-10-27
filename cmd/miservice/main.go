// Compatibility entry point; use cmd/maiservice for new builds.
//go:debug tlsrsakex=1

package main

import "miservice/internal/servicecli"

func main() { servicecli.Main() }
