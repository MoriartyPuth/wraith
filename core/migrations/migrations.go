// Package migrations embeds the ordered SQL schema migrations.
package migrations

import "embed"

// FS holds NNNN_name.sql files, applied in lexical order.
//
//go:embed *.sql
var FS embed.FS
