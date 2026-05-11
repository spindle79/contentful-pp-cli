// Copyright 2026 Adam Harris. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"contentful-pp-cli/internal/store"
)

// seedSQLTestDB creates a fresh on-disk SQLite store at a temp path and
// inserts a single content-types row so SELECT against it returns a known
// shape. Returns the path so newSqlCmd can be pointed at it via --db.
func seedSQLTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.db")

	s, err := store.OpenWithContext(context.Background(), path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// Top-level `id` is required because extractObjectID's lookup order
	// looks at id/ID/uuid/slug/name; without it we'd seed id="Blog Post"
	// (the fallback from `name`) and the test assertion would be a tautology.
	row := map[string]any{
		"id":   "blogPost",
		"sys":  map[string]any{"id": "blogPost", "type": "ContentType"},
		"name": "Blog Post",
	}
	raw, _ := json.Marshal(row)
	if err := s.UpsertContentTypes(raw); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	return path
}

// Happy path: a SELECT against the local mirror returns rows as JSON.
// Validates that newSqlCmd opens the store read-only and emits a JSON
// array (one element per row) when stdout is non-TTY / --json.
func TestSqlCmd_SelectReturnsRows(t *testing.T) {
	dbPath := seedSQLTestDB(t)

	flags := &rootFlags{asJSON: true}
	cmd := newSqlCmd(flags)
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--db", dbPath, "SELECT id, name FROM content_types"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(out, "[") {
		t.Fatalf("expected JSON array, got: %q", out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("unmarshal output: %v\nstdout=%q", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d (%v) raw=%q", len(rows), rows, out)
	}
	if got := rows[0]["id"]; got != "blogPost" {
		t.Errorf("id: want blogPost, got %v", got)
	}
}

// Security gate: any statement that isn't a SELECT or WITH...SELECT must
// be rejected with a usage error (exit code 2) before reaching the SQLite
// driver. Pinning this on the CLI surface matters because mode=ro alone
// does NOT block VACUUM INTO or ATTACH DATABASE — see the rationale in
// cliutil/readonly_sql.go.
func TestSqlCmd_RejectsNonSelect(t *testing.T) {
	dbPath := seedSQLTestDB(t)

	flags := &rootFlags{asJSON: true}
	cmd := newSqlCmd(flags)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--db", dbPath, "DROP TABLE content_types"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for non-SELECT query, got nil; stdout=%q", stdout.String())
	}
	if code := ExitCode(err); code != 2 {
		t.Errorf("expected usage exit code 2, got %d", code)
	}
}

// Comment-prefix bypass attempt: SQLite skips leading block comments and
// semicolons before parsing the first keyword, so the gate must strip
// those too. A naive HasPrefix check would let this through.
func TestSqlCmd_RejectsCommentPrefixedNonSelect(t *testing.T) {
	dbPath := seedSQLTestDB(t)

	flags := &rootFlags{asJSON: true}
	cmd := newSqlCmd(flags)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--db", dbPath, "/* sneaky */; ATTACH DATABASE '/tmp/x.db' AS evil"})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected comment-prefixed non-SELECT to be rejected")
	}
}
