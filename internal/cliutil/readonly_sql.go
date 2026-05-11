// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cliutil

import (
	"fmt"
	"strings"
)

// ValidateReadOnlyQuery gates ad-hoc SQL run against the local SQLite mirror,
// whether the entry point is the MCP `sql` tool or the CLI `sql` command. The
// agent contract advertised to the MCP host is ReadOnlyHintAnnotation(true);
// a false annotation on a mutating tool lets MCP hosts auto-approve writes
// and is treated as a real bug per the project's agent-native security model.
//
// The gate is an allowlist (SELECT or WITH only) applied AFTER stripping the
// leading whitespace, line comments, block comments, and semicolons that
// SQLite itself ignores before parsing. A naive HasPrefix check on a keyword
// blocklist is bypassable by prefixing the dangerous statement with
// "/* x */" or "-- x\n" — TrimSpace strips outer whitespace but does not
// understand SQL comment syntax. Combined with the empirical fact that
// modernc.org/sqlite's mode=ro does NOT block VACUUM INTO (writes a snapshot
// to a new file) or ATTACH DATABASE (opens a separate writable handle),
// such a bypass produces silent exfiltration to an attacker-chosen path.
//
// SELECT and WITH are the only allowed leading keywords. WITH supports
// SELECT-form CTEs; CTE-wrapped writes ("WITH x AS (...) INSERT ...") are
// caught by OpenReadOnly's mode=ro one layer down. PRAGMA, ATTACH, VACUUM,
// and every other DDL/DML keyword fail at this gate before reaching SQLite.
func ValidateReadOnlyQuery(query string) error {
	upper := strings.ToUpper(StripLeadingSQLNoise(query))
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return fmt.Errorf("only SELECT queries are allowed")
	}
	return nil
}

// StripLeadingSQLNoise removes leading whitespace, SQL line comments
// (-- to end of line), block comments (/* ... */), and statement separators
// (;) from query. SQLite skips these before parsing the first keyword, so a
// security gate that does not strip them mismatches what the driver actually
// executes.
func StripLeadingSQLNoise(query string) string {
	for {
		query = strings.TrimLeft(query, " \t\r\n;")
		switch {
		case strings.HasPrefix(query, "--"):
			if idx := strings.IndexByte(query, '\n'); idx >= 0 {
				query = query[idx+1:]
				continue
			}
			return ""
		case strings.HasPrefix(query, "/*"):
			if idx := strings.Index(query[2:], "*/"); idx >= 0 {
				query = query[2+idx+2:]
				continue
			}
			return ""
		default:
			return query
		}
	}
}
