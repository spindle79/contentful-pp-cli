// Copyright 2026 user. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"

	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

type brokenRefRow struct {
	SourceID    string `json:"source_id"`
	SourceField string `json:"source_field"`
	Locale      string `json:"locale,omitempty"`
	TargetID    string `json:"target_id"`
	TargetType  string `json:"target_type"`
	Reason      string `json:"reason"`
}

func newRefsBrokenCmd(flags *rootFlags) *cobra.Command {
	var localeFilter string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "refs-broken",
		Short: "Flag entries whose link fields point at deleted, archived, or unpublished targets",
		Long: `Walk the local mirror and report Link fields that point at:

  • a target id missing from the local mirror entirely (deleted or never synced)
  • a target with no sys.publishedVersion (unpublished or archived)

Use --locale <code> to limit the scan to a specific locale (default: all locales).`,
		Example:     "  contentful-pp-cli refs-broken --json\n  contentful-pp-cli refs-broken --locale en-US",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			path := dbPath
			if path == "" {
				path = defaultDBPath("contentful-pp-cli")
			}
			if !dbExists(path) {
				return fmt.Errorf("local store %s not found — run 'contentful-pp-cli sync' first", path)
			}
			st, err := store.OpenReadOnly(path)
			if err != nil {
				return fmt.Errorf("opening local store at %s: %w (have you run 'contentful-pp-cli sync'?)", path, err)
			}
			defer st.Close()

			// Existence + publish status
			entryStatus, err := loadPublishStatus(st, "entries")
			if err != nil {
				return err
			}
			assetStatus, err := loadPublishStatus(st, "assets")
			if err != nil {
				return err
			}

			erows, err := st.Query(`SELECT id, data FROM entries`)
			if err != nil {
				return fmt.Errorf("scanning entries: %w", err)
			}
			defer erows.Close()

			var rows []brokenRefRow
			for erows.Next() {
				var id string
				var raw []byte
				if err := erows.Scan(&id, &raw); err != nil {
					continue
				}
				var obj map[string]any
				if err := json.Unmarshal(raw, &obj); err != nil {
					continue
				}
				fields, _ := obj["fields"].(map[string]any)
				for fieldName, fieldVal := range fields {
					locales, ok := fieldVal.(map[string]any)
					if !ok {
						continue
					}
					for locale, locValue := range locales {
						if localeFilter != "" && locale != localeFilter {
							continue
						}
						checkValueForBroken(id, fieldName, locale, locValue, entryStatus, assetStatus, &rows)
					}
				}
			}

			if flags.asJSON || flags.csv || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			headers := []string{"SOURCE_ID", "FIELD", "LOCALE", "TARGET_ID", "TARGET_TYPE", "REASON"}
			tableRows := make([][]string, 0, len(rows))
			for _, r := range rows {
				tableRows = append(tableRows, []string{r.SourceID, r.SourceField, r.Locale, r.TargetID, r.TargetType, r.Reason})
			}
			if len(tableRows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no broken references found")
				return nil
			}
			return flags.printTable(cmd, headers, tableRows)
		},
	}
	cmd.Flags().StringVar(&localeFilter, "locale", "", "Restrict the scan to a single locale (e.g. en-US)")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path")
	return cmd
}

type publishStatus struct {
	Exists    bool
	Published bool
}

func loadPublishStatus(st *store.Store, table string) (map[string]publishStatus, error) {
	out := map[string]publishStatus{}
	rows, err := st.Query(fmt.Sprintf(`SELECT id, data FROM %s`, table))
	if err != nil {
		return out, nil // table may have no rows; treat as empty
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		st := publishStatus{Exists: true}
		if sys, ok := obj["sys"].(map[string]any); ok {
			if _, has := sys["publishedVersion"]; has {
				st.Published = true
			}
		}
		out[id] = st
	}
	return out, nil
}

func checkValueForBroken(srcID, field, locale string, v any, entryStatus, assetStatus map[string]publishStatus, out *[]brokenRefRow) {
	switch val := v.(type) {
	case map[string]any:
		if isLinkNode(val) {
			sysObj, _ := val["sys"].(map[string]any)
			lt, _ := sysObj["linkType"].(string)
			id, _ := sysObj["id"].(string)
			if id == "" {
				return
			}
			var status map[string]publishStatus
			switch lt {
			case "Entry":
				status = entryStatus
			case "Asset":
				status = assetStatus
			default:
				return
			}
			s, ok := status[id]
			if !ok {
				*out = append(*out, brokenRefRow{
					SourceID: srcID, SourceField: field, Locale: locale,
					TargetID: id, TargetType: lt, Reason: "missing",
				})
			} else if !s.Published {
				*out = append(*out, brokenRefRow{
					SourceID: srcID, SourceField: field, Locale: locale,
					TargetID: id, TargetType: lt, Reason: "unpublished",
				})
			}
			return
		}
		for _, child := range val {
			checkValueForBroken(srcID, field, locale, child, entryStatus, assetStatus, out)
		}
	case []any:
		for _, child := range val {
			checkValueForBroken(srcID, field, locale, child, entryStatus, assetStatus, out)
		}
	}
}
