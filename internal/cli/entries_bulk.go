// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"contentful-pp-cli/internal/cliutil"
	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// attachEntriesBulkCmd is no longer the registration site — entries.go now
// registers the bulk children directly so the verify-skill static parser can
// see them. Kept as an exported no-op for any out-of-tree caller.
func attachEntriesBulkCmd(parent *cobra.Command, flags *rootFlags) {
	_ = parent
	_ = flags
}

// newEntriesBulkPublishCmd, newEntriesBulkUnpublishCmd, and newEntriesBulkValidateCmd
// each declare a Cobra command with a *literal* Use string. verify-skill's command-tree
// resolver only recognizes string-literal Use fields, not concatenations — that's why
// each constructor exists as a separate function instead of a parameterized helper.
//
// The actual command body is shared via configureEntriesBulkAction.

func newEntriesBulkPublishCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bulk-publish <space_id> <environment_id>",
		Short:   "Bulk publish entries selected by SQL or @file (rate-aware against CMA bulk-action API)",
		Example: "  contentful-pp-cli entries bulk-publish 550e8400-e29b-41d4-a716-446655440000 master --where \"json_extract(data,'$.sys.contentType.sys.id') = 'article'\" --confirm\n  contentful-pp-cli entries bulk-publish 550e8400-e29b-41d4-a716-446655440000 master --ids @ids.txt",
	}
	configureEntriesBulkAction(cmd, flags, "publish")
	return cmd
}

func newEntriesBulkUnpublishCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bulk-unpublish <space_id> <environment_id>",
		Short:   "Bulk unpublish entries selected by SQL or @file (rate-aware against CMA bulk-action API)",
		Example: "  contentful-pp-cli entries bulk-unpublish 550e8400-e29b-41d4-a716-446655440000 master --where \"json_extract(data,'$.sys.contentType.sys.id') = 'article'\" --confirm\n  contentful-pp-cli entries bulk-unpublish 550e8400-e29b-41d4-a716-446655440000 master --ids @ids.txt",
	}
	configureEntriesBulkAction(cmd, flags, "unpublish")
	return cmd
}

func newEntriesBulkValidateCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "bulk-validate <space_id> <environment_id>",
		Short:   "Bulk validate entries selected by SQL or @file (rate-aware against CMA bulk-action API)",
		Example: "  contentful-pp-cli entries bulk-validate 550e8400-e29b-41d4-a716-446655440000 master --where \"json_extract(data,'$.sys.contentType.sys.id') = 'article'\" --confirm\n  contentful-pp-cli entries bulk-validate 550e8400-e29b-41d4-a716-446655440000 master --ids @ids.txt",
	}
	configureEntriesBulkAction(cmd, flags, "validate")
	return cmd
}

func configureEntriesBulkAction(cmd *cobra.Command, flags *rootFlags, action string) {
	var where, idsFile, dbPath string
	var confirm bool
	var ratePerSec float64
	// rateAware is on by default and the flag is a documented no-op kept for
	// recipe / SKILL.md compatibility. The adaptive limiter ALWAYS reads
	// X-Contentful-RateLimit-Reset and adjusts; --rate-limit-rps tunes the start.
	var rateAware bool
	_ = rateAware

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		if len(args) < 2 {
			return usageErr(fmt.Errorf("usage: %s <space_id> <environment_id>", cmd.CommandPath()))
		}
		spaceID, envID := args[0], args[1]

		// Default to dry-run unless --confirm was given. --dry-run wins.
		effectiveDryRun := flags.dryRun || !confirm

		// Pick id source: --ids wins if both are provided.
		path := dbPath
		if path == "" {
			path = defaultDBPath("contentful-pp-cli")
		}

		ids, err := selectBulkIDs(path, where, idsFile)
		if err != nil {
			// In a dry-run preview, report the error in-envelope rather
			// than failing — agents probing the command shouldn't see a
			// non-zero exit just because the local DB hasn't been synced.
			if effectiveDryRun {
				summary := map[string]any{
					"action":         action,
					"space_id":       spaceID,
					"environment_id": envID,
					"selected":       0,
					"dry_run":        true,
					"error":          err.Error(),
				}
				if flags.asJSON || !isTerminal(cmd.OutOrStdout()) {
					return printJSONFiltered(cmd.OutOrStdout(), summary, flags)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would %s 0 entries (dry-run; selection error: %v)\n", action, err)
				return nil
			}
			return err
		}

		summary := map[string]any{
			"action":         action,
			"space_id":       spaceID,
			"environment_id": envID,
			"selected":       len(ids),
			"dry_run":        effectiveDryRun,
		}

		if effectiveDryRun {
			summary["preview_ids"] = previewIDs(ids, 10)
			if flags.asJSON || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), summary, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "would %s %d entries (pass --confirm to execute)\n", action, len(ids))
			for _, id := range previewIDs(ids, 10) {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", id)
			}
			if len(ids) > 10 {
				fmt.Fprintf(cmd.OutOrStdout(), "  ... +%d more\n", len(ids)-10)
			}
			return nil
		}

		c, err := flags.newClient()
		if err != nil {
			return err
		}
		c = c.WithTier("cma")

		if ratePerSec <= 0 {
			ratePerSec = 5
		}
		limiter := cliutil.NewAdaptiveLimiter(ratePerSec)

		versions, err := loadEntryVersions(path, ids)
		if err != nil {
			return err
		}

		succeeded := 0
		failed := 0
		var perID []map[string]any
		for _, id := range ids {
			limiter.Wait()
			body := map[string]any{
				"entities": map[string]any{
					"sys":   map[string]any{"type": "Array"},
					"items": []any{linkItem(id, versions[id])},
				},
			}
			p := fmt.Sprintf("/spaces/%s/environments/%s/bulk_actions/%s", spaceID, envID, action)
			_, status, err := c.PostWithHeaders(p, body, map[string]string{"Content-Type": "application/vnd.contentful.management.v1+json"})
			row := map[string]any{"id": id, "status": status}
			if err != nil {
				failed++
				row["error"] = err.Error()
				if strings.Contains(err.Error(), "HTTP 429") {
					limiter.OnRateLimit()
				}
			} else {
				succeeded++
				limiter.OnSuccess()
			}
			perID = append(perID, row)
		}
		summary["succeeded"] = succeeded
		summary["failed"] = failed
		summary["results"] = perID

		if flags.asJSON || !isTerminal(cmd.OutOrStdout()) {
			return printJSONFiltered(cmd.OutOrStdout(), summary, flags)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %d succeeded, %d failed (selected %d)\n", action, succeeded, failed, len(ids))
		return nil
	}
	cmd.Flags().StringVar(&where, "where", "", "SQL WHERE expression against the local entries table (e.g. \"json_extract(data,'$.sys.contentType.sys.id') = 'article'\")")
	cmd.Flags().StringVar(&idsFile, "ids", "", "File of newline-delimited entry ids (prefix with @, e.g. @ids.txt)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Actually run the bulk action; without --confirm this is a dry-run preview")
	cmd.Flags().Float64Var(&ratePerSec, "rate-limit-rps", 5, "Adaptive limiter starting rate (Contentful CMA ceiling is ~7 rps)")
	cmd.Flags().BoolVar(&rateAware, "rate-aware", true, "Adaptive rate limiting respecting X-Contentful-RateLimit-* headers (default: on; flag kept for compatibility)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path")
}

func selectBulkIDs(dbPath, where, idsFile string) ([]string, error) {
	if idsFile != "" {
		path := strings.TrimPrefix(idsFile, "@")
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading ids file %s: %w", path, err)
		}
		var ids []string
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				ids = append(ids, line)
			}
		}
		return ids, nil
	}
	if where == "" {
		return nil, usageErr(fmt.Errorf("either --where <SQL> or --ids @file is required"))
	}
	if !dbExists(dbPath) {
		return nil, fmt.Errorf("local store %s not found — run 'contentful-pp-cli sync' first", dbPath)
	}
	st, err := store.OpenReadOnly(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening local store at %s: %w", dbPath, err)
	}
	defer st.Close()
	q := "SELECT id FROM entries WHERE " + where
	rows, err := st.Query(q)
	if err != nil {
		return nil, fmt.Errorf("running --where query: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func loadEntryVersions(dbPath string, ids []string) (map[string]int, error) {
	out := map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	st, err := store.OpenReadOnly(dbPath)
	if err != nil {
		return out, nil
	}
	defer st.Close()
	for _, id := range ids {
		rows, err := st.Query(`SELECT data FROM entries WHERE id = ?`, id)
		if err != nil {
			continue
		}
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				continue
			}
			var obj map[string]any
			if err := json.Unmarshal(raw, &obj); err != nil {
				continue
			}
			if sys, ok := obj["sys"].(map[string]any); ok {
				if v, ok := sys["version"].(float64); ok {
					out[id] = int(v)
				}
			}
		}
		rows.Close()
	}
	return out, nil
}

func linkItem(id string, version int) map[string]any {
	link := map[string]any{
		"sys": map[string]any{
			"type":     "Link",
			"linkType": "Entry",
			"id":       id,
		},
	}
	if version > 0 {
		link["sys"].(map[string]any)["version"] = version
	}
	return link
}

func previewIDs(ids []string, n int) []string {
	if len(ids) <= n {
		return ids
	}
	return ids[:n]
}
