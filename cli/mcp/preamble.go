package mcp

import _ "embed"

// Preamble is the fixed family guidance shared by every catalog MCP server.
//
//go:embed preamble.md
var Preamble string
