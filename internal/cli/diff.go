// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

// diffOutcome carries the structured diff so callers can post-process or
// pipe to migrate-gen.
type diffOutcome struct {
	EnvA       string                 `json:"environment_a"`
	EnvB       string                 `json:"environment_b"`
	Sections   map[string]*diffBucket `json:"sections"`
	Structural bool                   `json:"structural"`
	Content    bool                   `json:"content"`
	ExitCode   int                    `json:"exit_code"`
}

type diffBucket struct {
	OnlyInA  []string         `json:"only_in_a"`
	OnlyInB  []string         `json:"only_in_b"`
	Modified []diffModifiedID `json:"modified"`
}

type diffModifiedID struct {
	ID    string `json:"id"`
	HashA string `json:"hash_a"`
	HashB string `json:"hash_b"`
}

func newDiffCmd(flags *rootFlags) *cobra.Command {
	var include string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "diff <env-a> <env-b>",
		Short: "Diff two environments across the local mirror (entries, content types, releases, ...)",
		Long: `Diff two synced environments across the surfaces contentful-merge skips:
content types, entries, releases, scheduled actions, tags, tasks, roles.

Both environments must already be in the local mirror (run sync against each
first). Exit codes:

  0  no diff
  1  content-only diff (entries differ, content types match)
  2  structural diff (content types or roles changed)`,
		Example:     "  contentful-pp-cli diff master staging --json\n  contentful-pp-cli diff master staging --include releases,tags",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if len(args) < 2 {
				return usageErr(fmt.Errorf("usage: %s <env-a> <env-b>", cmd.CommandPath()))
			}
			if dryRunOK(flags) {
				return nil
			}
			envA, envB := args[0], args[1]

			path := dbPath
			if path == "" {
				path = defaultDBPath("contentful-pp-cli")
			}
			if !dbExists(path) {
				return fmt.Errorf("local store %s not found — run 'contentful-pp-cli sync' first", path)
			}
			st, err := store.OpenReadOnly(path)
			if err != nil {
				return fmt.Errorf("opening local store at %s: %w", path, err)
			}
			defer st.Close()

			sections := defaultDiffSections()
			if include != "" {
				for _, s := range strings.Split(include, ",") {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					sections = appendUnique(sections, s)
				}
			}

			result := &diffOutcome{EnvA: envA, EnvB: envB, Sections: map[string]*diffBucket{}}

			for _, sec := range sections {
				table := tableForSection(sec)
				if table == "" {
					continue
				}
				bucket, presence, err := diffTableByEnv(st, table, envA, envB)
				if err != nil {
					return err
				}
				result.Sections[sec] = bucket
				switch presence {
				case "missing_a":
					return fmt.Errorf("environment %q has no %s rows in local mirror — run 'contentful-pp-cli sync' against it first", envA, table)
				case "missing_b":
					return fmt.Errorf("environment %q has no %s rows in local mirror — run 'contentful-pp-cli sync' against it first", envB, table)
				}
			}

			for sec, b := range result.Sections {
				if len(b.OnlyInA) == 0 && len(b.OnlyInB) == 0 && len(b.Modified) == 0 {
					continue
				}
				switch sec {
				case "content_types", "roles", "locales":
					result.Structural = true
				default:
					result.Content = true
				}
			}
			result.ExitCode = 0
			if result.Structural {
				result.ExitCode = 2
			} else if result.Content {
				result.ExitCode = 1
			}

			if flags.asJSON || flags.csv || !isTerminal(cmd.OutOrStdout()) {
				if err := printJSONFiltered(cmd.OutOrStdout(), result, flags); err != nil {
					return err
				}
			} else {
				renderDiffHuman(cmd, result)
			}
			if result.ExitCode == 0 {
				return nil
			}
			return &cliError{code: result.ExitCode, err: fmt.Errorf("diff: structural=%v content=%v", result.Structural, result.Content)}
		},
	}
	cmd.Flags().StringVar(&include, "include", "", "Comma-separated extra sections to include (releases,scheduled-actions,tags,tasks,roles); content_types and entries always included")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path")
	return cmd
}

func defaultDiffSections() []string {
	return []string{"content_types", "entries"}
}

// tableForSection maps the user-facing section name onto the underlying table.
// Unknown values return "" so the caller silently skips them.
func tableForSection(s string) string {
	switch s {
	case "entries":
		return "entries"
	case "content_types", "content-types", "contentTypes":
		return "content_types"
	case "releases":
		return "releases"
	case "scheduled-actions", "scheduled_actions":
		return "scheduled_actions"
	case "tags":
		return "tags"
	case "tasks":
		return "tasks"
	case "roles":
		return "roles"
	case "locales":
		return "locales"
	}
	return ""
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

func diffTableByEnv(st *store.Store, table, envA, envB string) (*diffBucket, string, error) {
	dataA, err := loadEnvRows(st, table, envA)
	if err != nil {
		return nil, "", err
	}
	dataB, err := loadEnvRows(st, table, envB)
	if err != nil {
		return nil, "", err
	}
	if len(dataA) == 0 && len(dataB) == 0 {
		// Both empty — neither environment has data for this table; treat as
		// equal rather than missing (avoids spurious "missing" errors when an
		// environment legitimately has no releases, etc.).
		return &diffBucket{}, "", nil
	}
	bucket := &diffBucket{}
	for id, ha := range dataA {
		hb, ok := dataB[id]
		if !ok {
			bucket.OnlyInA = append(bucket.OnlyInA, id)
		} else if ha != hb {
			bucket.Modified = append(bucket.Modified, diffModifiedID{ID: id, HashA: ha, HashB: hb})
		}
	}
	for id := range dataB {
		if _, ok := dataA[id]; !ok {
			bucket.OnlyInB = append(bucket.OnlyInB, id)
		}
	}
	sort.Strings(bucket.OnlyInA)
	sort.Strings(bucket.OnlyInB)
	sort.Slice(bucket.Modified, func(i, j int) bool { return bucket.Modified[i].ID < bucket.Modified[j].ID })
	return bucket, "", nil
}

// loadEnvRows scans <table> filtering by parent_id == env. For tables that
// don't carry parent_id (e.g. roles is org-scoped), we fall back to scanning
// every row when the env-filtered set is empty.
func loadEnvRows(st *store.Store, table, env string) (map[string]string, error) {
	out := map[string]string{}
	rows, err := st.Query(fmt.Sprintf(`SELECT id, data FROM %s WHERE parent_id = ?`, table), env)
	if err != nil {
		// table may lack parent_id column; fall back
		rows, err = st.Query(fmt.Sprintf(`SELECT id, data FROM %s`, table))
		if err != nil {
			return out, nil
		}
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		out[id] = hashForDiff(raw)
	}
	return out, nil
}

// hashForDiff returns a stable hash of the JSON payload, after stripping
// fields that change on every fetch and would otherwise mark every row as
// modified (sys.version, sys.updatedAt, sys.revision, etc.).
func hashForDiff(raw []byte) string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:8])
	}
	if sys, ok := obj["sys"].(map[string]any); ok {
		for _, k := range []string{"version", "updatedAt", "revision", "publishedCounter", "updatedBy"} {
			delete(sys, k)
		}
	}
	canon, _ := json.Marshal(obj)
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:8])
}

func renderDiffHuman(cmd *cobra.Command, result *diffOutcome) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "diff %s ↔ %s\n", result.EnvA, result.EnvB)
	keys := make([]string, 0, len(result.Sections))
	for k := range result.Sections {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, sec := range keys {
		b := result.Sections[sec]
		if len(b.OnlyInA) == 0 && len(b.OnlyInB) == 0 && len(b.Modified) == 0 {
			fmt.Fprintf(w, "  %-20s match\n", sec)
			continue
		}
		fmt.Fprintf(w, "  %-20s only_in_a=%d only_in_b=%d modified=%d\n", sec, len(b.OnlyInA), len(b.OnlyInB), len(b.Modified))
	}
}
