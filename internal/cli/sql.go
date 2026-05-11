// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"contentful-pp-cli/internal/cliutil"
	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// newSqlCmd registers `contentful-pp-cli sql` — ad-hoc read-only SQL against
// the local SQLite mirror. The MCP server exposes the same capability as a
// tool; the CLI command is the Bash-agent path (clis-worker invokes printed
// CLIs over Bash with --agent, not over MCP).
//
// Validation is shared with the MCP tool via cliutil.ValidateReadOnlyQuery so
// the same comment-stripping allowlist guards both surfaces — a regression in
// one would skew the other.
func newSqlCmd(flags *rootFlags) *cobra.Command {
	var (
		query  string
		dbPath string
	)

	cmd := &cobra.Command{
		Use:   "sql <query>",
		Short: "Run a read-only SQL query against the local SQLite mirror",
		Long: `Run an ad-hoc SQL SELECT (or WITH...SELECT) against the local
SQLite mirror populated by 'contentful-pp-cli sync'. The database is opened
read-only; non-SELECT statements are rejected before reaching SQLite.

Use this for aggregations and joins across resources that the per-endpoint
commands cannot express in one call — orphan counts, field-fill rate per
content type, locale coverage, etc.`,
		Example: `  # Count entries per content type
  contentful-pp-cli sql "SELECT content_type_id, COUNT(*) FROM entries GROUP BY content_type_id"

  # Pipe through jq
  contentful-pp-cli sql "SELECT id, name FROM content_types" --json | jq '.[].name'

  # Via --query flag
  contentful-pp-cli sql --query "SELECT * FROM webhooks LIMIT 5"`,
		Annotations: map[string]string{"mcp:hidden": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve the query from either --query or the first positional arg.
			q := strings.TrimSpace(query)
			if q == "" && len(args) > 0 {
				q = strings.TrimSpace(args[0])
			}
			if q == "" {
				return usageErr(fmt.Errorf("sql requires a query (positional arg or --query)"))
			}

			// Reject non-SELECT statements at the same gate the MCP tool uses.
			// validateReadOnlyQuery returns a generic "only SELECT" error;
			// wrap as usageErr so the CLI exits with code 2 (typed exit codes
			// matter to the agent harness that pipes this out).
			if err := cliutil.ValidateReadOnlyQuery(q); err != nil {
				return usageErr(err)
			}

			if dbPath == "" {
				dbPath = defaultDBPath("contentful-pp-cli")
			}
			if !dbExists(dbPath) {
				return notFoundErr(fmt.Errorf("local database not found at %s\nRun 'contentful-pp-cli sync' first to populate it.", dbPath))
			}

			db, err := store.OpenReadOnly(dbPath)
			if err != nil {
				return serverErr(fmt.Errorf("opening database: %w", err))
			}
			defer db.Close()

			rows, err := db.Query(q)
			if err != nil {
				return serverErr(fmt.Errorf("query failed: %w", err))
			}
			defer rows.Close()

			cols, err := rows.Columns()
			if err != nil {
				return serverErr(fmt.Errorf("reading columns: %w", err))
			}

			var results []map[string]any
			for rows.Next() {
				values := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range values {
					ptrs[i] = &values[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return serverErr(fmt.Errorf("scanning row: %w", err))
				}
				row := make(map[string]any, len(cols))
				for i, col := range cols {
					row[col] = sqlCellValue(values[i])
				}
				results = append(results, row)
			}
			if err := rows.Err(); err != nil {
				return serverErr(fmt.Errorf("iterating rows: %w", err))
			}

			// Results slice may be nil when no rows match; coerce to [] so
			// JSON consumers see a valid empty array rather than `null`.
			if results == nil {
				results = []map[string]any{}
			}

			// JSON mode: stream out an array. The PersistentPreRunE auto-JSON
			// flip means asJSON is already true when stdout is piped, so this
			// is the dominant path for agents.
			if flags.asJSON || !isTerminal(cmd.OutOrStdout()) {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}

			// TTY: render as a table via the shared auto-table helper.
			if len(results) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "(no rows)")
				return nil
			}
			return printAutoTable(cmd.OutOrStdout(), results)
		},
	}

	cmd.Flags().StringVar(&query, "query", "", "SQL query (alternative to positional arg)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path (default: ~/.local/share/contentful-pp-cli/data.db)")

	return cmd
}

// sqlCellValue normalizes the []byte values that SQLite drivers return for
// TEXT and BLOB columns. Without this, JSON output renders TEXT columns as
// base64 strings (encoding/json's default for []byte) — surprising to agents
// who expect "id":"blogPost" rather than "id":"YmxvZ1Bvc3Q=".
func sqlCellValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
