// Copyright 2026 user. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"sort"
	"strings"

	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

type refNode struct {
	ID          string     `json:"id"`
	ContentType string     `json:"contentType,omitempty"`
	Title       string     `json:"title,omitempty"`
	Depth       int        `json:"depth"`
	Children    []*refNode `json:"children,omitempty"`
}

func newRefsCmd(flags *rootFlags) *cobra.Command {
	var depth int
	var reverse bool
	var format string
	var dbPath string

	cmd := &cobra.Command{
		Use:   "refs <entry-id>",
		Short: "Walk the local references graph forward (or reverse) from an entry",
		Long: `Walk the local references graph from an entry, up to --depth levels.

Default direction is "forward": what does this entry link to.
With --reverse: what links to this entry.`,
		Example:     "  contentful-pp-cli refs 5K8aXghc8eUqYKGm0IqsAU --depth 2\n  contentful-pp-cli refs 5K8aXghc8eUqYKGm0IqsAU --reverse --json\n  contentful-pp-cli refs 5K8aXghc8eUqYKGm0IqsAU --format dot",
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			entryID := args[0]

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

			refs, entryMeta, _, _, err := loadReferenceGraph(st)
			if err != nil {
				return err
			}

			// Build reverse index lazily — only when needed.
			var inbound map[string][]string
			if reverse {
				inbound = map[string][]string{}
				for src, edges := range refs {
					for _, e := range edges {
						if e.Type == "Entry" {
							inbound[e.ID] = append(inbound[e.ID], src)
						}
					}
				}
			}

			// Reject unknown ids with a typed not-found error so agents and
			// shells can distinguish "no references found" from "entry doesn't
			// exist in the local mirror".
			meta, exists := entryMeta[entryID]
			if !exists {
				return notFoundErr(fmt.Errorf("entry %s not found in local mirror — run 'contentful-pp-cli sync' or check the entry ID", entryID))
			}
			root := &refNode{ID: entryID, Depth: 0}
			root.ContentType = meta.ContentType
			root.Title = meta.Title
			_ = exists

			visited := map[string]bool{entryID: true}
			queue := []*refNode{root}
			for len(queue) > 0 {
				next := []*refNode{}
				for _, node := range queue {
					if node.Depth >= depth {
						continue
					}
					var neighbors []string
					if reverse {
						neighbors = inbound[node.ID]
					} else {
						for _, e := range refs[node.ID] {
							if e.Type == "Entry" {
								neighbors = append(neighbors, e.ID)
							}
						}
					}
					sort.Strings(neighbors)
					for _, nb := range neighbors {
						if visited[nb] {
							continue
						}
						visited[nb] = true
						child := &refNode{ID: nb, Depth: node.Depth + 1}
						if m, ok := entryMeta[nb]; ok {
							child.ContentType = m.ContentType
							child.Title = m.Title
						}
						node.Children = append(node.Children, child)
						next = append(next, child)
					}
				}
				queue = next
			}

			switch strings.ToLower(format) {
			case "dot":
				return renderRefsDot(cmd, root, reverse)
			}

			if flags.asJSON || flags.csv || !isTerminal(cmd.OutOrStdout()) {
				return printJSONFiltered(cmd.OutOrStdout(), root, flags)
			}
			renderRefsTree(cmd, root, "", true)
			return nil
		},
	}
	cmd.Flags().IntVar(&depth, "depth", 1, "Maximum traversal depth from the starting entry")
	cmd.Flags().BoolVar(&reverse, "reverse", false, "Walk reverse references (what links to this entry)")
	cmd.Flags().StringVar(&format, "format", "tree", "Output format: tree, dot")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path (default ~/.local/share/contentful-pp-cli/data.db)")
	return cmd
}

func renderRefsTree(cmd *cobra.Command, n *refNode, prefix string, isLast bool) {
	w := cmd.OutOrStdout()
	connector := "├── "
	nextPrefix := prefix + "│   "
	if isLast {
		connector = "└── "
		nextPrefix = prefix + "    "
	}
	if n.Depth == 0 {
		fmt.Fprintf(w, "%s [%s] %s\n", n.ID, n.ContentType, n.Title)
	} else {
		fmt.Fprintf(w, "%s%s%s [%s] %s\n", prefix, connector, n.ID, n.ContentType, n.Title)
	}
	for i, child := range n.Children {
		renderRefsTree(cmd, child, nextPrefix, i == len(n.Children)-1)
	}
}

func renderRefsDot(cmd *cobra.Command, root *refNode, reverse bool) error {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "digraph refs {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box, fontname=\"Helvetica\"];")
	var emit func(n *refNode)
	emit = func(n *refNode) {
		label := n.ID
		if n.Title != "" {
			label = fmt.Sprintf("%s\\n%s", n.Title, n.ID)
		}
		fmt.Fprintf(w, "  %q [label=%q];\n", n.ID, label)
		for _, c := range n.Children {
			if reverse {
				fmt.Fprintf(w, "  %q -> %q;\n", c.ID, n.ID)
			} else {
				fmt.Fprintf(w, "  %q -> %q;\n", n.ID, c.ID)
			}
			emit(c)
		}
	}
	emit(root)
	fmt.Fprintln(w, "}")
	return nil
}
