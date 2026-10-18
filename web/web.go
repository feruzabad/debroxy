// Package web embeds the built browser client (ADR 0022). Only cmd imports it.
package web

import "embed"

// Dist is the client build; a committed placeholder index.html keeps it
// compiling before the client is built.
//
//go:embed all:dist
var Dist embed.FS
