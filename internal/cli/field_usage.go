// Copyright 2026 user. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

type fieldUsageReport struct {
	ContentType    string         `json:"content_type"`
	Field          string         `json:"field"`
	TotalEntries   int            `json:"total_entries"`
	PerLocale      map[string]int `json:"populated_per_locale"`
	ShapeHistogram map[string]int `json:"value_shape_histogram"`
	OrphanField    bool           `json:"orphan_field"`
	DefinedInCT    bool           `json:"defined_in_content_type"`
	Note           string         `json:"note,omitempty"`
}

func newFieldUsageCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	cmd := &cobra.Command{
		Use:   "field-usage <content-type>.<field>",
		Short: "Per-locale fill rate, value-shape histogram, and orphan-field detection for a field",
		Long: `Compute per-locale fill rate and a value-shape histogram for a single field
on a content type, using the local mirror.

If the content type's schema declares the field but no entry uses it, the
report flags it as an "orphan field".`,
		Example:     "  contentful-pp-cli field-usage article.heroImage --json\n  contentful-pp-cli field-usage landingPage.slug",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			parts := strings.SplitN(args[0], ".", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return usageErr(fmt.Errorf("expected <content-type>.<field>, got %q", args[0]))
			}
			ct, fieldID := parts[0], parts[1]

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

			report := fieldUsageReport{
				ContentType:    ct,
				Field:          fieldID,
				PerLocale:      map[string]int{},
				ShapeHistogram: map[string]int{},
			}

			// Check the content type schema (CMA first, then CDA mirror)
			report.DefinedInCT = fieldDefinedInContentType(st, ct, fieldID)

			erows, err := st.Query(`SELECT data FROM entries`)
			if err != nil {
				return fmt.Errorf("scanning entries: %w", err)
			}
			defer erows.Close()

			for erows.Next() {
				var raw []byte
				if err := erows.Scan(&raw); err != nil {
					continue
				}
				var obj map[string]any
				if err := json.Unmarshal(raw, &obj); err != nil {
					continue
				}
				if entryContentType(obj) != ct {
					continue
				}
				report.TotalEntries++
				fields, _ := obj["fields"].(map[string]any)
				val, ok := fields[fieldID]
				if !ok {
					continue
				}
				locales, ok := val.(map[string]any)
				if !ok {
					// Field present but not localized — count under "*"
					report.PerLocale["*"]++
					report.ShapeHistogram[shapeOf(val)]++
					continue
				}
				for loc, lv := range locales {
					if isPopulated(lv) {
						report.PerLocale[loc]++
					}
					report.ShapeHistogram[shapeOf(lv)]++
				}
			}

			if report.DefinedInCT && report.TotalEntries > 0 {
				populated := 0
				for _, n := range report.PerLocale {
					populated += n
				}
				if populated == 0 {
					report.OrphanField = true
					report.Note = "field declared on content type but no entry populates it in any locale"
				}
			}

			if flags.asJSON || flags.csv || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), report, flags)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Content type: %s   Field: %s\n", report.ContentType, report.Field)
			fmt.Fprintf(cmd.OutOrStdout(), "  defined in content-type schema: %v\n", report.DefinedInCT)
			fmt.Fprintf(cmd.OutOrStdout(), "  total entries of this type:    %d\n", report.TotalEntries)
			fmt.Fprintln(cmd.OutOrStdout(), "  populated per locale:")
			localeKeys := make([]string, 0, len(report.PerLocale))
			for k := range report.PerLocale {
				localeKeys = append(localeKeys, k)
			}
			sort.Strings(localeKeys)
			for _, k := range localeKeys {
				fmt.Fprintf(cmd.OutOrStdout(), "    %-12s %d\n", k, report.PerLocale[k])
			}
			fmt.Fprintln(cmd.OutOrStdout(), "  value-shape histogram:")
			shapeKeys := make([]string, 0, len(report.ShapeHistogram))
			for k := range report.ShapeHistogram {
				shapeKeys = append(shapeKeys, k)
			}
			sort.Strings(shapeKeys)
			for _, k := range shapeKeys {
				fmt.Fprintf(cmd.OutOrStdout(), "    %-12s %d\n", k, report.ShapeHistogram[k])
			}
			if report.OrphanField {
				fmt.Fprintln(cmd.OutOrStdout(), "  orphan_field: true — declared on the content type but unused")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path")
	return cmd
}

func entryContentType(entry map[string]any) string {
	sys, ok := entry["sys"].(map[string]any)
	if !ok {
		return ""
	}
	ct, ok := sys["contentType"].(map[string]any)
	if !ok {
		return ""
	}
	csys, ok := ct["sys"].(map[string]any)
	if !ok {
		return ""
	}
	if id, ok := csys["id"].(string); ok {
		return id
	}
	return ""
}

func fieldDefinedInContentType(st *store.Store, ct, fieldID string) bool {
	for _, table := range []string{"content_types", "delivery_content_types"} {
		rows, err := st.Query(fmt.Sprintf(`SELECT data FROM %s WHERE id = ?`, table), ct)
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
			fields, _ := obj["fields"].([]any)
			for _, fAny := range fields {
				f, _ := fAny.(map[string]any)
				if id, _ := f["id"].(string); id == fieldID {
					rows.Close()
					return true
				}
			}
		}
		rows.Close()
	}
	return false
}

func shapeOf(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		// JSON numbers always decode to float64; classify as int when integral.
		if val == float64(int64(val)) {
			return "int"
		}
		return "float"
	case []any:
		return "array"
	case map[string]any:
		// Detect Link / RichText shapes for richer reporting.
		if isLinkNode(val) {
			return "link"
		}
		if t, _ := val["nodeType"].(string); t != "" {
			return "richtext"
		}
		return "object"
	default:
		return "unknown"
	}
}

func isPopulated(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case string:
		return val != ""
	case []any:
		return len(val) > 0
	case map[string]any:
		return len(val) > 0
	}
	return true
}
