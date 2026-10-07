// Package ui embeds the investigation panel that the MCP server serves as
// the ui://logchef/investigation.html resource.
package ui

import _ "embed"

// InvestigationHTML is rebuilt with `bun run build` in this directory.
//
//go:embed investigation.html
var InvestigationHTML string
