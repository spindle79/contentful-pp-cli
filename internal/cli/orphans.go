// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"

	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// orphanRow is the per-row output of the orphans command. The shape is
// stable across --entries / --types / --assets so callers can union all
// three modes into a single JSON array.
type orphanRow struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // "entry", "asset", or "content_type"
	ContentType string `json:"contentType,omitempty"`
	Title       string `json:"title,omitempty"`
}

func newOrphansCmd(flags *rootFlags) *cobra.Command {
	var entriesFlag, typesFlag, assetsFlag bool
	var dbPath string

	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Find entries, content types, or assets with no incoming references in the local mirror",
		Long: `Find entries, content types, or assets with no incoming references in the local mirror.

orphans scans the local SQLite mirror (populated by 'contentful-pp-cli sync') and
walks every entry's link fields to build an in-memory references graph. Anything
the graph never points at is reported as orphaned.

If no scope flag is set, --entries is the default.`,
		Example:     "  contentful-pp-cli orphans --entries\n  contentful-pp-cli orphans --types --json\n  contentful-pp-cli orphans --entries --assets --json --select id,kind,title",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !entriesFlag && !typesFlag && !assetsFlag {
				entriesFlag = true
			}
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

			refs, entryMeta, assetMeta, ctMeta, err := loadReferenceGraph(st)
			if err != nil {
				return err
			}

			incomingEntry := map[string]int{}
			incomingAsset := map[string]int{}
			incomingCT := map[string]int{}
			for _, edges := range refs {
				for _, tgt := range edges {
					switch tgt.Type {
					case "Entry":
						incomingEntry[tgt.ID]++
					case "Asset":
						incomingAsset[tgt.ID]++
					}
				}
			}
			// Content types are referenced implicitly by entries: every entry's
			// sys.contentType.sys.id is a "use" of a content type. Count those too.
			for _, m := range entryMeta {
				if m.ContentType != "" {
					incomingCT[m.ContentType]++
				}
			}

			var rows []orphanRow
			if entriesFlag {
				for id, m := range entryMeta {
					if incomingEntry[id] == 0 {
						rows = append(rows, orphanRow{ID: id, Kind: "entry", ContentType: m.ContentType, Title: m.Title})
					}
				}
			}
			if assetsFlag {
				for id, m := range assetMeta {
					if incomingAsset[id] == 0 {
						rows = append(rows, orphanRow{ID: id, Kind: "asset", Title: m.Title})
					}
				}
			}
			if typesFlag {
				for id, m := range ctMeta {
					if incomingCT[id] == 0 {
						rows = append(rows, orphanRow{ID: id, Kind: "content_type", Title: m.Name})
					}
				}
			}

			if flags.asJSON || flags.csv || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}

			headers := []string{"KIND", "ID", "CONTENT_TYPE", "TITLE"}
			tableRows := make([][]string, 0, len(rows))
			for _, r := range rows {
				tableRows = append(tableRows, []string{r.Kind, r.ID, r.ContentType, r.Title})
			}
			if len(tableRows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no orphans found")
				return nil
			}
			return flags.printTable(cmd, headers, tableRows)
		},
	}

	cmd.Flags().BoolVar(&entriesFlag, "entries", false, "Scan for orphaned entries (default if none of --entries/--types/--assets is set)")
	cmd.Flags().BoolVar(&typesFlag, "types", false, "Scan for content types with zero entries")
	cmd.Flags().BoolVar(&assetsFlag, "assets", false, "Scan for orphaned assets")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path (default ~/.local/share/contentful-pp-cli/data.db)")
	return cmd
}

// linkRef is one outgoing edge from a source entry to a target Entry/Asset.
type linkRef struct {
	Type string // "Entry" or "Asset"
	ID   string
}

type entryMetaRow struct {
	ContentType string
	Title       string
}

type assetMetaRow struct {
	Title string
}

type ctMetaRow struct {
	Name string
}

// loadReferenceGraph walks entries, builds the outgoing-link graph, and also
// returns small metadata maps for entries / assets / content types so callers
// can render rows with friendly titles.
func loadReferenceGraph(st *store.Store) (map[string][]linkRef, map[string]entryMetaRow, map[string]assetMetaRow, map[string]ctMetaRow, error) {
	refs := map[string][]linkRef{}
	entryMeta := map[string]entryMetaRow{}
	assetMeta := map[string]assetMetaRow{}
	ctMeta := map[string]ctMetaRow{}

	// Entries: id, data
	rows, err := st.Query(`SELECT id, data FROM entries`)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("scanning entries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, nil, nil, nil, err
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		ct, title := extractEntryMeta(obj)
		entryMeta[id] = entryMetaRow{ContentType: ct, Title: title}
		links := extractLinks(obj)
		if len(links) > 0 {
			refs[id] = links
		}
	}

	// Assets
	arows, err := st.Query(`SELECT id, data FROM assets`)
	if err == nil {
		defer arows.Close()
		for arows.Next() {
			var id string
			var raw []byte
			if err := arows.Scan(&id, &raw); err != nil {
				continue
			}
			var obj map[string]any
			_ = json.Unmarshal(raw, &obj)
			assetMeta[id] = assetMetaRow{Title: extractAssetTitle(obj)}
		}
	}

	// Content types
	crows, err := st.Query(`SELECT id, data FROM content_types`)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var id string
			var raw []byte
			if err := crows.Scan(&id, &raw); err != nil {
				continue
			}
			var obj map[string]any
			_ = json.Unmarshal(raw, &obj)
			ctMeta[id] = ctMetaRow{Name: stringField(obj, "name")}
		}
	}

	return refs, entryMeta, assetMeta, ctMeta, nil
}

// extractLinks walks a Contentful entry payload and returns every Link
// reference under fields. Handles per-locale field shapes (each locale value
// can itself be a Link, an array of Links, or a richer rich-text payload).
func extractLinks(entry map[string]any) []linkRef {
	var out []linkRef
	fields, _ := entry["fields"].(map[string]any)
	for _, raw := range fields {
		walkLinks(raw, &out)
	}
	return out
}

func walkLinks(v any, out *[]linkRef) {
	switch val := v.(type) {
	case map[string]any:
		// Direct Link node?
		if isLinkNode(val) {
			sysObj, _ := val["sys"].(map[string]any)
			lt, _ := sysObj["linkType"].(string)
			id, _ := sysObj["id"].(string)
			if (lt == "Entry" || lt == "Asset") && id != "" {
				*out = append(*out, linkRef{Type: lt, ID: id})
			}
			return
		}
		for _, child := range val {
			walkLinks(child, out)
		}
	case []any:
		for _, child := range val {
			walkLinks(child, out)
		}
	}
}

func isLinkNode(m map[string]any) bool {
	sysObj, ok := m["sys"].(map[string]any)
	if !ok {
		return false
	}
	t, _ := sysObj["type"].(string)
	return t == "Link"
}

func extractEntryMeta(entry map[string]any) (contentType, title string) {
	if sys, ok := entry["sys"].(map[string]any); ok {
		if ct, ok := sys["contentType"].(map[string]any); ok {
			if csys, ok := ct["sys"].(map[string]any); ok {
				if id, ok := csys["id"].(string); ok {
					contentType = id
				}
			}
		}
	}
	// Try common display field names. We can't know the content type's
	// displayField without joining content_types; this best-effort heuristic
	// covers the common cases.
	if fields, ok := entry["fields"].(map[string]any); ok {
		for _, name := range []string{"title", "name", "internalName", "slug"} {
			if v, ok := fields[name].(map[string]any); ok {
				for _, locVal := range v {
					if s, ok := locVal.(string); ok && s != "" {
						title = s
						return
					}
				}
			}
		}
	}
	return
}

func extractAssetTitle(asset map[string]any) string {
	fields, _ := asset["fields"].(map[string]any)
	if t, ok := fields["title"].(map[string]any); ok {
		for _, v := range t {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	if f, ok := fields["file"].(map[string]any); ok {
		for _, v := range f {
			if file, ok := v.(map[string]any); ok {
				if name, ok := file["fileName"].(string); ok && name != "" {
					return name
				}
			}
		}
	}
	return ""
}

func stringField(obj map[string]any, key string) string {
	s, _ := obj[key].(string)
	return s
}
