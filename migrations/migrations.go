// Package migrations embeds the goose migrations into the binary (ADR 0015).
// It declares nothing but Files; only cmd imports it.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
