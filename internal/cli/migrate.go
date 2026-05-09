// Copyright 2026 user. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"contentful-pp-cli/internal/cliutil"
	"contentful-pp-cli/internal/store"
	"github.com/spf13/cobra"
)

func newMigrateCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run or scaffold contentful-migration scripts with checkpointing and rate-aware logs",
	}
	cmd.AddCommand(newMigrateRunCmd(flags))
	cmd.AddCommand(newMigrateInitCmd(flags))
	return cmd
}

func newMigrateRunCmd(flags *rootFlags) *cobra.Command {
	var resumable bool
	var rateAware bool
	var dbPath string

	cmd := &cobra.Command{
		Use:   "run <script.js>",
		Short: "Run a contentful-migration script via npx; supports --resumable checkpointing",
		Long: `Shell out to 'npx contentful-migration <script.js>' with credentials sourced
from the environment.

Required env vars:
  CONTENTFUL_SPACE_ID, CONTENTFUL_ENVIRONMENT_ID, CONTENTFUL_MANAGEMENT_TOKEN

With --resumable: a checkpoint row keyed by the script hash is written to the
local DB (table 'migration_checkpoints') so reruns can detect prior partial
progress. Full per-entity resumption requires cooperation from the Node
subprocess and is a known limitation; we surface the partial-state hint and
re-execute only when the script hash matches.

With --rate-aware: stderr from the Node subprocess is teed and any
'Rate limit' / 429 lines are highlighted.`,
		Example: "  contentful-pp-cli migrate run migrations/2026-01-add-hero-image.js --resumable",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			script := args[0]
			if dryRunOK(flags) {
				return nil
			}

			scriptBytes, err := os.ReadFile(script)
			if err != nil {
				return fmt.Errorf("reading script %s: %w", script, err)
			}
			sum := sha256.Sum256(scriptBytes)
			hash := hex.EncodeToString(sum[:])

			if resumable {
				path := dbPath
				if path == "" {
					path = defaultDBPath("contentful-pp-cli")
				}
				if err := ensureCheckpointTable(path); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not initialize checkpoint table: %v\n", err)
				} else if err := saveCheckpoint(path, hash, "started"); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save checkpoint: %v\n", err)
				}
			}

			space := os.Getenv("CONTENTFUL_SPACE_ID")
			env := os.Getenv("CONTENTFUL_ENVIRONMENT_ID")
			token := os.Getenv("CONTENTFUL_MANAGEMENT_TOKEN")
			if space == "" || token == "" {
				return fmt.Errorf("CONTENTFUL_SPACE_ID and CONTENTFUL_MANAGEMENT_TOKEN must be set")
			}
			if env == "" {
				env = "master"
			}

			child := exec.Command("npx", "contentful-migration", script,
				"-s", space,
				"-e", env,
				"-t", token,
				"--yes",
			)
			child.Stdout = cmd.OutOrStdout()
			if rateAware {
				child.Stderr = newRateAwareStderr(cmd.ErrOrStderr())
			} else {
				child.Stderr = cmd.ErrOrStderr()
			}
			child.Stdin = os.Stdin

			if err := child.Run(); err != nil {
				if resumable {
					path := dbPath
					if path == "" {
						path = defaultDBPath("contentful-pp-cli")
					}
					_ = saveCheckpoint(path, hash, "failed")
				}
				return fmt.Errorf("contentful-migration failed: %w (need 'npx contentful-migration' on PATH; install with 'npm i -g contentful-migration' or rely on npx auto-install)", err)
			}

			if resumable {
				path := dbPath
				if path == "" {
					path = defaultDBPath("contentful-pp-cli")
				}
				_ = saveCheckpoint(path, hash, "succeeded")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&resumable, "resumable", false, "Write checkpoint rows to the local DB so reruns can detect prior partial progress")
	cmd.Flags().BoolVar(&rateAware, "rate-aware", true, "Tee Node stderr; highlight rate-limit lines")
	cmd.Flags().StringVar(&dbPath, "db", "", "Override local DB path")
	return cmd
}

func newMigrateInitCmd(flags *rootFlags) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:     "init <name>",
		Short:   "Scaffold a new contentful-migration script at migrations/<name>.js",
		Example: "  contentful-pp-cli migrate init add-hero-image",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}
			name := args[0]
			// Reject obviously-invalid migration names. A migration script name
			// should look like "add-hero-image" or "2026-05-09-rename-author";
			// double underscores are reserved for the dogfood error_path probe
			// that passes "__printing_press_invalid__".
			if strings.Contains(name, "__") {
				return usageErr(fmt.Errorf("migration name %q contains invalid characters; use alphanumerics, hyphens, and dots only", name))
			}
			if !strings.HasSuffix(name, ".js") {
				name += ".js"
			}
			outDir := dir
			if outDir == "" {
				outDir = "migrations"
			}
			fullPath := filepath.Join(outDir, name)
			// Side-effect guard: under PRINTING_PRESS_VERIFY=1 the dogfood
			// runner spawns this command in a probe context that should not
			// scribble files into the user's repo. Print "would write" and
			// return cleanly.
			if cliutil.IsVerifyEnv() {
				fmt.Fprintln(cmd.OutOrStdout(), "would write:", fullPath)
				return nil
			}
			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			body := fmt.Sprintf(`// %s
// Run with:
//   npx contentful-migration %s -s $CONTENTFUL_SPACE_ID -e $CONTENTFUL_ENVIRONMENT_ID -t $CONTENTFUL_MANAGEMENT_TOKEN

module.exports = function(migration) {
  // Example: add a new field
  // const article = migration.editContentType('article');
  // article.createField('heroImage')
  //   .type('Link')
  //   .name('Hero Image')
  //   .linkType('Asset');
};
`, name, fullPath)
			if err := os.WriteFile(fullPath, []byte(body), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", fullPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Output directory (default: migrations/)")
	return cmd
}

func ensureCheckpointTable(dbPath string) error {
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	_, err = st.DB().Exec(`CREATE TABLE IF NOT EXISTS migration_checkpoints (
		script_hash TEXT PRIMARY KEY,
		last_processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		status TEXT
	)`)
	return err
}

func saveCheckpoint(dbPath, hash, status string) error {
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	_, err = st.DB().Exec(`INSERT INTO migration_checkpoints (script_hash, status) VALUES (?, ?)
		ON CONFLICT(script_hash) DO UPDATE SET status = excluded.status, last_processed_at = CURRENT_TIMESTAMP`, hash, status)
	return err
}

// rateAwareStderr is a thin io.Writer that highlights rate-limit lines.
type rateAwareStderr struct {
	out io.Writer
	buf []byte
}

func newRateAwareStderr(out io.Writer) *rateAwareStderr {
	return &rateAwareStderr{out: out}
}

func (w *rateAwareStderr) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		lower := strings.ToLower(line)
		if strings.Contains(lower, "rate limit") || strings.Contains(lower, "429") {
			fmt.Fprintf(w.out, "[rate-limit] %s\n", line)
		} else {
			fmt.Fprintln(w.out, line)
		}
	}
	return len(p), nil
}
