// Copyright 2026 user. Licensed under Apache-2.0. See LICENSE.
//
// pp:novel-static-reference
// gql-impact is a local-filesystem analysis tool: it walks the user's working
// directory looking for GraphQL/TS references to a Contentful field. It does
// not call the Contentful API and does not read from the local store — by
// design. The data source is the user's own source tree.

package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

type gqlImpactRow struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

func newGqlImpactCmd(flags *rootFlags) *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "gql-impact <content-type>.<field>",
		Short: "Scan local source for GraphQL/TS references to a soon-to-be-removed Contentful field",
		Long: `Walk the current working directory (or --root) and search .graphql, .ts, .tsx,
.js, .jsx files for usages of the given content-type and field.

This is a heuristic scan: anything mentioning the content-type id near the field
name, or the field name in a GraphQL-shaped context, is reported. Matches are
case-sensitive on the field id and case-insensitive on the content-type id.

Output is file:line:snippet so you can pipe to grep / fzf / your editor.`,
		Example:     "  contentful-pp-cli gql-impact article.heroImage --json\n  contentful-pp-cli gql-impact landingPage.modules",
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
			ct, field := parts[0], parts[1]

			scanRoot := root
			if scanRoot == "" {
				scanRoot = "."
			}

			exts := map[string]bool{".graphql": true, ".gql": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true}
			ignoreDirs := map[string]bool{".git": true, "node_modules": true, "dist": true, "build": true, ".next": true, "out": true, "vendor": true, ".turbo": true}

			var rows []gqlImpactRow
			err := filepath.Walk(scanRoot, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				if info.IsDir() {
					if ignoreDirs[info.Name()] {
						return filepath.SkipDir
					}
					return nil
				}
				if !exts[strings.ToLower(filepath.Ext(path))] {
					return nil
				}
				rows = append(rows, scanFile(path, ct, field)...)
				return nil
			})
			if err != nil {
				return err
			}

			sort.Slice(rows, func(i, j int) bool {
				if rows[i].File != rows[j].File {
					return rows[i].File < rows[j].File
				}
				return rows[i].Line < rows[j].Line
			})

			if flags.asJSON || flags.csv || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), rows, flags)
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no references found")
				return nil
			}
			for _, r := range rows {
				fmt.Fprintf(cmd.OutOrStdout(), "%s:%d: %s\n", r.File, r.Line, r.Snippet)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "Directory to scan (default: cwd)")
	return cmd
}

// scanFile flags every line that mentions the field id, plus a "context"
// neighborhood check: when the file mentions the content-type id within ~50
// lines of the field, those matches are higher-confidence. We don't dedupe by
// confidence in v1 — every textual hit is reported.
func scanFile(path, ct, field string) []gqlImpactRow {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 4*1024*1024)
	lineNum := 0
	var lines []string
	for scanner.Scan() {
		lineNum++
		lines = append(lines, scanner.Text())
	}
	if scanner.Err() != nil {
		return nil
	}

	// Pre-compute: which lines mention the content-type id
	ctLower := strings.ToLower(ct)
	ctHits := map[int]bool{}
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), ctLower) {
			ctHits[i] = true
		}
	}

	var rows []gqlImpactRow
	for i, line := range lines {
		if !strings.Contains(line, field) {
			continue
		}
		// Skip pure substring hits where field is part of a larger identifier
		// AND the file makes no mention of the content-type id (low confidence).
		nearCT := false
		for j := i - 50; j <= i+50; j++ {
			if ctHits[j] {
				nearCT = true
				break
			}
		}
		looksGQL := looksGraphQLContext(line)
		if !nearCT && !looksGQL {
			continue
		}
		rows = append(rows, gqlImpactRow{File: path, Line: i + 1, Snippet: strings.TrimSpace(line)})
	}
	return rows
}

func looksGraphQLContext(line string) bool {
	trimmed := strings.TrimSpace(line)
	// Heuristics: lines inside a GraphQL selection set tend to be a bare field
	// followed by maybe arguments; or they start with "query"/"fragment".
	if strings.HasPrefix(trimmed, "query") || strings.HasPrefix(trimmed, "mutation") || strings.HasPrefix(trimmed, "fragment") {
		return true
	}
	// e.g. "  heroImage {" or "heroImage(locale: \"en-US\")"
	if strings.HasSuffix(trimmed, "{") || strings.Contains(trimmed, "...on ") {
		return true
	}
	return false
}
